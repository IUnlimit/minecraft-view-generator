package com.illtamer.minecraft;

import javax.imageio.ImageIO;
import java.awt.AlphaComposite;
import java.awt.Color;
import java.awt.Graphics2D;
import java.awt.RenderingHints;
import java.awt.Shape;
import java.awt.Font;
import java.awt.FontFormatException;
import java.awt.font.FontRenderContext;
import java.awt.font.GlyphVector;
import java.awt.geom.AffineTransform;
import java.awt.geom.Path2D;
import java.awt.image.BufferedImage;
import java.awt.image.DataBufferInt;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Base64;
import java.util.Comparator;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;
import java.util.Set;

    /** Deterministic CPU renderer for the Minecraft 1.21.11 resource format. */
final class Renderer {
    /**
     * Synthetic texture id of the generated missing texture. The missing model samples it and every
     * lookup whose file is absent resolves to it, so unlike a real id it never needs a file.
     */
    private static final String MISSING_TEXTURE = "minecraft:missing";

    private final Map<String, Model> models = new HashMap<>();
    /**
     * The models of the custom items this request names, under a synthetic id so that every call site
     * resolves them exactly like a vanilla model. They come from the packs of the user content
     * directory, whose files are not part of the resource tree.
     */
    private final Map<String, Model> inlineModels = new HashMap<>();
    private MinecraftFont font;
    private ResourceStack resources;
    /**
     * How many device pixels one layout pixel occupies.
     *
     * <p>Every coordinate in this renderer is written in the layout space of the vanilla screen,
     * where one unit is one pixel at GUI scale one. Only {@link #canvas}, {@link #graphics} and
     * {@link #device} know about the scale, which is what lets a magnified picture be rasterised at
     * its final resolution — a bigger glyph, a bigger item icon, a bigger 3D model — instead of
     * drawing the native picture and blowing it up afterwards.</p>
     */
    private int pixelScale = 1;

    BufferedImage render(Map<String, Object> request) throws IOException {
        String operation = Json.string(request, "operation", "");
        String resourceRoot = Json.string(request, "resourceRoot", "");
        if (resourceRoot.isBlank()) {
            throw new IOException("resource context is not ready");
        }
        this.resources = ResourceStack.from(request, resourceRoot);
        this.pixelScale = Json.integer(request, "pixelScale", 1);
        if (pixelScale < 1 || pixelScale > 16) {
            throw new IOException("pixelScale must be between 1 and 16");
        }
        applyCustomArt(request);
        return switch (operation) {
            case "item" -> renderItem(Json.object(request.get("item"), "item"), Json.integer(request, "width", 128));
            case "player" -> renderPlayer(request, Json.integer(request, "width", 256), Json.integer(request, "height", 384));
            case "tooltip" -> renderTooltip(request, Json.integer(request, "width", 360), Json.integer(request, "height", 0));
            case "inventory" -> renderContainer(request, Json.integer(request, "width", 0), Json.integer(request, "height", 0), "inventory");
            case "item_container" -> renderSpecialContainer(request, Json.integer(request, "width", 0), Json.integer(request, "height", 0));
            case "enderchest" -> renderEnderchest(request, Json.integer(request, "width", 0), Json.integer(request, "height", 0));
            case "player_inventory" -> renderPlayerInventory(request, Json.integer(request, "width", 0), Json.integer(request, "height", 0));
            case "playerlist" -> renderPlayerList(request, Json.integer(request, "width", 0), Json.integer(request, "height", 0));
            case "map" -> renderMap(request, Json.integer(request, "width", 0), Json.integer(request, "height", 0));
            case "achievement" -> renderAchievement(request, Json.integer(request, "width", 480), Json.integer(request, "height", 96));
            default -> throw new IOException("unsupported renderer operation " + operation);
        };
    }

    /**
     * Allocates a canvas of the layout size at the render scale. Every buffer that is drawn in
     * vanilla screen coordinates comes from here, so the scale never leaks into the layout.
     */
    private BufferedImage canvas(int layoutWidth, int layoutHeight) throws IOException {
        int width = device(layoutWidth);
        int height = device(layoutHeight);
        if (width < 1 || height < 1 || width > 8192 || height > 8192) {
            throw new IOException("canvas is outside limits after the render scale: " + width + "x" + height);
        }
        return new BufferedImage(width, height, BufferedImage.TYPE_INT_ARGB);
    }

    /**
     * The Graphics2D of a canvas. The render scale lives in this transform, so every draw call below
     * keeps writing the vanilla screen coordinates it is laid out in while landing on the device
     * pixels of the final picture.
     */
    private Graphics2D graphics(BufferedImage canvas) {
        Graphics2D graphics = canvas.createGraphics();
        graphics.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_NEAREST_NEIGHBOR);
        if (pixelScale != 1) {
            graphics.scale(pixelScale, pixelScale);
        }
        return graphics;
    }

    /** One layout extent — a coordinate, a width, a glyph size — in device pixels. */
    private int device(int layout) {
        return layout * pixelScale;
    }

    /**
     * Renders one item icon. Structural request problems — no id, an impossible size — stay hard
     * errors, while anything that only concerns the resource pack (a missing model, an unsupported
     * special model, a texture that is not there) degrades to the vanilla missing model so that one
     * broken slot can never take the whole picture down.
     */
    private BufferedImage renderItem(Map<String, Object> item, int size) throws IOException {
        if (size < 16 || size > 4096) {
            throw new IOException("item render size must be between 16 and 4096");
        }
        // An icon is drawn at the resolution it is delivered at: every drawable of the item pipeline
        // scales with `size`, so handing it the device size costs nothing but is what keeps a
        // magnified slot icon sharp.
        int pixelSize = device(size);
        if (pixelSize > 4096) {
            throw new IOException("item render size is outside limits after the render scale");
        }
        String id = normalizeID(Json.string(item, "id", ""));
        if (id.isBlank()) {
            throw new IOException("item.id is required");
        }
        BufferedImage result;
        try {
            result = renderItemIcon(item, id, pixelSize);
            if (Json.integer(item, "damage", 0) > 0 && Json.integer(item, "maxDamage", 0) > 0) {
                drawDurability(result, Json.integer(item, "damage", 0), Json.integer(item, "maxDamage", 0));
            }
            if (hasGlint(item)) {
                drawGlint(result);
            }
        } catch (IOException | RuntimeException error) {
            System.err.println("item " + id + " fell back to the missing model: " + error);
            result = renderMissingItemIcon(pixelSize);
        }
        return result;
    }

    private BufferedImage renderItemIcon(Map<String, Object> item, String id, int size) throws IOException {
        ItemSelection selection = resolveItemSelection(id, item);
        if (selection.special != null) {
            return renderSpecialItemIcon(selection.special, item, size);
        }
        Model model = loadModelWithOverrides(selection.model, item);
        if (model.isGenerated()) {
            if (model.generatedLayers().isEmpty()) {
                // Vanilla bakes an item model without geometry as the missing model.
                return renderMissingItemIcon(size);
            }
            List<BufferedImage> layers = new ArrayList<>();
            for (String layer : model.generatedLayers()) {
                layers.add(resources.texture(model.texture(layer, layer)));
            }
            return generatedSprite(layers, size);
        }
        if (!model.elements.isEmpty()) {
            return renderModel(model, size);
        }
        return renderMissingItemIcon(size);
    }

    private Model loadModelWithOverrides(String modelID, Map<String, Object> item) throws IOException {
        if (modelID.isBlank()) {
            modelID = "minecraft:item/" + itemIdPath(Json.string(item, "id", ""));
        }
        Model model = loadModel(modelID);
        for (Override override : model.overrides) {
            if (override.matches(item)) {
                model = loadModel(override.model);
                break;
            }
        }
        return model;
    }

    /**
     * Registers the custom items of this request: their models under a synthetic id, and their
     * textures in the resource stack under the id the model references. A custom model inherits from
     * the vanilla model that ended its parent chain, so it keeps the display transforms the hand slots
     * need without the renderer ever reading a pack directory.
     */
    private void applyCustomArt(Map<String, Object> request) {
        // The table belongs to one request: a render without custom art must never draw the custom
        // item a previous render registered.
        inlineModels.clear();
        if (!(request.get("customArt") instanceof Map<?, ?> artMap)) {
            return;
        }
        for (Map.Entry<?, ?> entry : artMap.entrySet()) {
            if (!(entry.getKey() instanceof String key) || !(entry.getValue() instanceof Map<?, ?> rawArt)) {
                continue;
            }
            Map<String, Object> art = castMap(rawArt);
            if (art.get("model") instanceof Map<?, ?> rawModel) {
                String id = customModelID(key);
                try {
                    inlineModels.put(id, loadInlineModel(id, castMap(rawModel)));
                } catch (IOException | RuntimeException error) {
                    System.err.println("custom item " + key + " could not be loaded: " + error.getMessage());
                }
            }
            if (art.get("textures") instanceof Map<?, ?> textures) {
                for (Map.Entry<?, ?> texture : textures.entrySet()) {
                    if (texture.getKey() instanceof String id && texture.getValue() instanceof String encoded) {
                        resources.putInlineTexture(id, encoded);
                    }
                }
            }
        }
    }

    /**
     * The model id a custom item is drawn from, or an empty string when this request names no custom
     * item for the given id and custom model data value.
     */
    private String customModelSelection(String id, Map<String, Object> item) {
        Object value = item.get("customData");
        if (!(value instanceof Number number)) {
            return "";
        }
        String key = itemIdPath(normalizeID(id)).toUpperCase(java.util.Locale.ROOT) + "#" + number.intValue();
        String model = customModelID(key);
        return inlineModels.containsKey(model) ? model : "";
    }

    /** The synthetic model id of one custom item: its base material and its custom model data value. */
    private static String customModelID(String key) {
        return "custom:" + key;
    }

    /**
     * Loads an inline model and the vanilla model its chain ended on. A parent that cannot be read is
     * not fatal: everything the parent would have contributed from the pack is already flattened into
     * the inline model.
     */
    private Model loadInlineModel(String id, Map<String, Object> raw) throws IOException {
        Model own = Model.from(id, raw);
        String parentID = own.parent;
        Model base = null;
        if (!parentID.isBlank() && !parentID.startsWith("builtin/")) {
            try {
                base = loadModel(normalizeID(parentID));
            } catch (IOException error) {
                System.err.println("custom item " + id + " inherits from the missing model " + parentID);
            }
        }
        Model merged = base == null ? new Model(id) : base.copyFor(id);
        merged.merge(own);
        return merged;
    }

    private ItemSelection resolveItemSelection(String id, Map<String, Object> item) throws IOException {
        String custom = customModelSelection(id, item);
        if (!custom.isBlank()) {
            return new ItemSelection(custom, null);
        }
        String component = itemModelComponent(item);
        if (!component.isBlank()) {
            return new ItemSelection(component, null);
        }
        Map<String, Object> definition = resources.json(itemDefinitionPath(id));
        if (definition != null) {
            ItemSelection selected = selectItemSelection(definition.get("model"), item);
            if (selected.present()) {
                return selected;
            }
            throw new IOException("item definition has no supported model: " + id);
        }
        return new ItemSelection("minecraft:item/" + itemIdPath(id), null);
    }

    private ItemSelection selectItemSelection(Object value, Map<String, Object> item) throws IOException {
        if (value instanceof String model) {
            return new ItemSelection(normalizeID(model), null);
        }
        if (!(value instanceof Map<?, ?> map)) {
            return new ItemSelection("", null);
        }
        Map<String, Object> object = castMap(map);
        String type = Json.string(object, "type", "").toLowerCase();
        if (type.equals("minecraft:model") || type.equals("model")) {
            return new ItemSelection(normalizeID(Json.string(object, "model", "")), null);
        }
        if (type.equals("minecraft:special") || type.equals("special")) {
            // The family of a special model is the "type" of the nested model object; the base field
            // only names the model whose display transforms the item borrows.
            Map<String, Object> parameters = object.get("model") instanceof Map<?, ?> nested
                    ? castMap(nested) : Map.of();
            String family = textureName(normalizeID(Json.string(parameters, "type",
                    Json.string(object, "special", ""))));
            if (family.isBlank()) {
                family = textureName(normalizeID(Json.string(object, "base", "")));
            }
            return new ItemSelection("", new SpecialItem(family, parameters, normalizeID(Json.string(object, "base", ""))));
        }
        if (type.equals("minecraft:condition") || type.equals("condition")) {
            String property = Json.string(object, "property", "").toLowerCase();
            boolean condition = switch (property) {
                case "minecraft:using_item", "using_item" -> Json.bool(item, "usingItem", false);
                case "minecraft:broken", "broken" -> Json.bool(item, "broken", false);
                case "minecraft:has_component", "has_component" -> item.get("components") instanceof Map<?, ?>;
                default -> Json.bool(item, property.replace("minecraft:", ""), false);
            };
            return selectItemSelection(condition ? object.get("on_true") : object.get("on_false"), item);
        }
        if (type.equals("minecraft:range_dispatch") || type.equals("range_dispatch")) {
            String property = Json.string(object, "property", "").toLowerCase();
            double actual = switch (property) {
                case "minecraft:custom_model_data", "custom_model_data" -> Json.number(item, "customModelData", 0);
                case "minecraft:damage", "damage" -> {
                    double damage = Json.number(item, "damage", 0);
                    double max = Json.number(item, "maxDamage", 0);
                    yield max > 0 ? damage / max : damage;
                }
                default -> Json.number(item, property.replace("minecraft:", ""), 0);
            };
            ItemSelection selected = null;
            double threshold = Double.NEGATIVE_INFINITY;
            if (object.get("entries") instanceof List<?> entries) {
                for (Object entryValue : entries) {
                    if (!(entryValue instanceof Map<?, ?> entryRaw)) continue;
                    Map<String, Object> entry = castMap(entryRaw);
                    double candidate = Json.number(entry, "threshold", Double.POSITIVE_INFINITY);
                    if (candidate <= actual && candidate >= threshold) {
                        threshold = candidate;
                        selected = selectItemSelection(entry.get("model"), item);
                    }
                }
            }
            return selected != null && selected.present()
                    ? selected : selectItemSelection(object.get("fallback"), item);
        }
        if (type.equals("minecraft:select") || type.equals("select")) {
            String property = Json.string(object, "property", "").toLowerCase();
            String actual = selectPropertyValue(property, item);
            if (object.get("cases") instanceof List<?> cases) {
                for (Object caseValue : cases) {
                    if (!(caseValue instanceof Map<?, ?> caseRaw)) continue;
                    Map<String, Object> entry = castMap(caseRaw);
                    Object when = entry.get("when");
                    boolean matches = when instanceof String s && s.equalsIgnoreCase(actual)
                            || when instanceof List<?> values && values.stream().anyMatch(v -> String.valueOf(v).equalsIgnoreCase(actual));
                    if (matches) return selectItemSelection(entry.get("model"), item);
                }
            }
            return selectItemSelection(object.get("fallback"), item);
        }
        return new ItemSelection("", null);
    }

    private String selectPropertyValue(String property, Map<String, Object> item) {
        String key = property.replace("minecraft:", "");
        Object value = item.get(key);
        if (value == null && item.get("components") instanceof Map<?, ?> raw) {
            Map<?, ?> components = raw;
            value = components.get(property);
            if (value == null) value = components.get("minecraft:" + key);
            if (value == null && key.equals("trim_material")) {
                Object trim = components.get("minecraft:trim");
                if (trim == null) trim = components.get("trim");
                if (trim instanceof Map<?, ?> trimMap) {
                    value = trimMap.get("material");
                    if (value == null) value = trimMap.get("minecraft:material");
                }
            }
        }
        if (value == null) {
            value = switch (key) {
                case "trim_material" -> item.getOrDefault("trimMaterial", "");
                case "display_context" -> item.getOrDefault("displayContext", "gui");
                default -> "";
            };
        }
        return String.valueOf(value).replace("minecraft:", "");
    }

    private String itemIdPath(String id) {
        int separator = id.indexOf(':');
        return separator < 0 ? id : id.substring(separator + 1);
    }

    /** The model an item resolves to: either a model JSON path or a special model of its own. */
    private record ItemSelection(String model, SpecialItem special) {
        boolean present() {
            return !model.isBlank() || special != null;
        }
    }

    /**
     * One {@code minecraft:special} item model. Vanilla describes these models in client code instead
     * of in a model JSON, so {@code family} selects one of the transcribed box lists, {@code parameters}
     * carries the {@code texture}, {@code color}, {@code kind} and {@code pose} fields of the
     * definition and {@code base} names the model the item borrows its display transforms from.
     */
    private record SpecialItem(String family, Map<String, Object> parameters, String base) {
        String parameter(String name) {
            Object value = parameters.get(name);
            return value == null ? "" : String.valueOf(value);
        }
    }

    /** One colour layer of a banner or a shield: a pattern sprite and the dye colour it carries. */
    private record PatternLayer(String name, int color) {}

    /**
     * Vanilla missing model: one 16³ cube whose six faces sample the missing texture, which is what
     * the client draws for an item whose model could not be baked. It is built here instead of loaded
     * from a file so that the fallback works inside the minimal resource context of this service.
     */
    private static Model missingModel() {
        Model model = new Model("builtin/missing");
        model.hasElements = true;
        LinkedHashMap<String, FaceDefinition> faces = new LinkedHashMap<>();
        for (String direction : new String[]{"down", "up", "north", "south", "west", "east"}) {
            faces.put(direction, new FaceDefinition(MISSING_TEXTURE, -1, null, 0));
        }
        model.elements.add(new Element(new double[]{0, 0, 0}, new double[]{16, 16, 16}, faces));
        return model;
    }

    /**
     * The vanilla missing model, projected by the straight icon camera into a square icon. The model
     * carries no display entry of its own, so the pose of an item without one centres its 0..16 box.
     */
    private BufferedImage renderMissingItemIcon(int size) throws IOException {
        List<Quad> quads = new ArrayList<>();
        addElementGeometry(quads, missingModel());
        transformQuads(quads, displayMatrix(null, false));
        return renderItemIcon(quads, size);
    }

    /**
     * The families this service still draws as the legacy flat sprite of their entity texture: the
     * chest family is a block model of its own and keeps the plane it used before this round, so an
     * existing consumer sees no change for it.
     */
    private static final Set<String> FLAT_SPECIAL_FAMILIES = Set.of("chest", "ender_chest");

    /** The sixteen dye colours in {@code DyeColor} order with their vanilla texture diffuse colours. */
    private static final String[] DYE_NAMES = {
            "white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
            "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"};
    private static final int[] DYE_COLORS = {
            0xF0F0F0, 0xEB8844, 0xC354CD, 0x6689D3, 0xDECF2A, 0x41CD34, 0xD88198, 0x434343,
            0xABABAB, 0x287697, 0x7B2FBE, 0x253192, 0x51301A, 0x3B511A, 0xB3312C, 0x1E1B1B};

    /** Every face of a vanilla {@code addBox} box, which is what the overload defaults to. */
    private static final Set<String> ALL_FACES = Set.of("down", "up", "north", "south", "west", "east");

    /** An empty face set, written out to read well at the call sites that skip nothing. */
    private static final Set<String> NO_FACES = Set.of();

    /** The flat sprite of the special families this service has not ported to geometry yet. */
    private String specialItemTexture(String family) throws IOException {
        return switch (family.toLowerCase(Locale.ROOT)) {
            case "ender_chest" -> "minecraft:entity/chest/ender";
            case "chest" -> "minecraft:entity/chest/normal";
            default -> throw new IOException("unsupported special item model: " + family);
        };
    }

    /**
     * The icon of a special item: its geometry in the vanilla item space, projected by the {@code gui}
     * pose of the model the item points at and then by the straight icon camera. A family this service
     * has not ported yet falls back to the missing model of {@link #renderItem}.
     */
    private BufferedImage renderSpecialItemIcon(SpecialItem special, Map<String, Object> item, int size) throws IOException {
        if (FLAT_SPECIAL_FAMILIES.contains(special.family)) {
            return generatedSprite(List.of(resources.texture(specialItemTexture(special.family))), size);
        }
        List<Quad> quads = new ArrayList<>();
        addSpecialGeometry(quads, special, item);
        transformQuads(quads, displayMatrix(specialDisplay(special, "gui"), false));
        return renderItemIcon(quads, size);
    }

    /** Dispatches to the transcribed geometry of one special model family. */
    private void addSpecialGeometry(List<Quad> quads, SpecialItem special, Map<String, Object> item) throws IOException {
        switch (special.family) {
            case "banner" -> addBannerGeometry(quads, special, item);
            case "bed" -> addBedGeometry(quads, special);
            case "shulker_box" -> addShulkerBoxGeometry(quads, special);
            case "head" -> addHeadGeometry(quads, special, item);
            case "player_head" -> addPlayerHeadGeometry(quads, item);
            case "shield" -> addShieldGeometry(quads, item);
            case "conduit" -> addConduitGeometry(quads);
            case "decorated_pot" -> addDecoratedPotGeometry(quads, item);
            case "copper_golem_statue" -> addCopperGolemGeometry(quads, special);
            case "trident" -> addTridentGeometry(quads);
            default -> throw new IOException("unsupported special item model: " + special.family);
        }
    }

    /** The display transform a special item borrows from the model its {@code base} field names. */
    private DisplayTransform specialDisplay(SpecialItem special, String name) throws IOException {
        if (special.base.isBlank()) {
            return null;
        }
        return loadModel(special.base).displayTransform(name);
    }

    /**
     * Vanilla 1.21.11 {@code hrd} (BannerSpecialRenderer) with {@code hgi} (BannerFlagModel) and
     * {@code hgj} (BannerModel): {@code translate(0.5, 0, 0.5); scale(2/3, -2/3, -2/3)}. The pole and
     * the bar sample {@code entity/banner_base}; the flag is drawn with that same texture and then once
     * per colour layer — the base colour of the item against {@code entity/banner/base} and every
     * banner pattern against its own sprite — which is the layer order of
     * {@code BannerRenderer#renderPatterns}.
     */
    private void addBannerGeometry(List<Quad> quads, SpecialItem special, Map<String, Object> item) throws IOException {
        BufferedImage structure = resources.texture("minecraft:entity/banner_base");
        // hdf.o = standing_banner: hgj, pole texOffs(44, 0) box(-1, -42, -1, 2, 42, 2) and
        // bar texOffs(0, 42) box(-10, -44, -1, 20, 2, 2), both at PartPose.ZERO.
        addVanillaBox(quads, Mat4.identity(), new double[]{-1, -42, -1}, new double[]{1, 0, 1}, new double[]{2, 42, 2}, structure, 44, 0, PartPose.ZERO);
        addVanillaBox(quads, Mat4.identity(), new double[]{-10, -44, -1}, new double[]{10, -42, 1}, new double[]{20, 2, 2}, structure, 0, 42, PartPose.ZERO);
        // hdf.p = standing_banner flag: hgi, flag texOffs(0, 0) box(-10, 0, -2, 20, 40, 1) at
        // PartPose.offset(0, -44, 0).
        BufferedImage base = resources.texture("minecraft:entity/banner/base");
        addBannerFlagLayer(quads, structure, 0xFFFFFF);
        addBannerFlagLayer(quads, base, bannerColor(special, item));
        for (PatternLayer layer : bannerPatterns(item, "entity/banner")) {
            addBannerFlagLayer(quads, resources.texture("minecraft:entity/banner/" + layer.name()), layer.color());
        }
        transformQuads(quads, Mat4.translation(8, 0, 8).multiply(Mat4.scaling(0.6666667, -0.6666667, -0.6666667)));
    }

    /** One pass of the banner flag, tinted with the diffuse colour of a banner layer. */
    private void addBannerFlagLayer(List<Quad> quads, BufferedImage texture, int color) {
        List<Quad> flag = new ArrayList<>();
        addVanillaBox(flag, Mat4.identity(), new double[]{-10, 0, -2}, new double[]{10, 40, -1}, new double[]{20, 40, 1},
                dye(texture, color), 0, 0, 0, false, PartPose.offset(0, -44, 0), ALL_FACES);
        quads.addAll(flag);
    }

    /**
     * Vanilla {@code hrf} (BedRenderer) with the {@code hdf.u} head and {@code hdf.t} foot layers:
     * {@code translate(0, 0.5625, foot ? -1 : 0); rotateX(90); translate(0.5, 0.5, 0.5);
     * rotateZ(180 + facing.toYRot()); translate(-0.5, -0.5, -0.5)}. The item context renders both
     * halves with the vanilla south facing, so the z rotation is a full turn and the two centring
     * translations cancel, which leaves the tilt below.
     */
    private void addBedGeometry(List<Quad> quads, SpecialItem special) throws IOException {
        String color = textureName(normalizeID(special.parameter("texture")));
        BufferedImage texture = resources.texture("minecraft:entity/bed/" + (color.isBlank() ? "white" : color));
        for (boolean foot : new boolean[]{false, true}) {
            List<Quad> half = new ArrayList<>();
            if (foot) {
                // hrf.d(): main texOffs(0, 22) box(0, 0, 0, 16, 16, 6) at PartPose.ZERO,
                // left_leg texOffs(50, 0) box(0, 6, -16, 3, 3, 3) at rotation(90, 0, 0),
                // right_leg texOffs(50, 12) box(-16, 6, -16, 3, 3, 3) at rotation(90, 0, 270).
                addVanillaBox(half, Mat4.identity(), new double[]{0, 0, 0}, new double[]{16, 16, 6}, new double[]{16, 16, 6}, texture, 0, 22, PartPose.ZERO);
                addVanillaBox(half, Mat4.identity(), new double[]{0, 6, -16}, new double[]{3, 9, -13}, new double[]{3, 3, 3}, texture, 50, 0, PartPose.rotation(Math.PI / 2, 0, 0));
                addVanillaBox(half, Mat4.identity(), new double[]{-16, 6, -16}, new double[]{-13, 9, -13}, new double[]{3, 3, 3}, texture, 50, 12, PartPose.rotation(Math.PI / 2, 0, Math.PI * 1.5));
            } else {
                // hrf.a(): main texOffs(0, 0) box(0, 0, 0, 16, 16, 6) at PartPose.ZERO,
                // left_leg texOffs(50, 6) box(0, 6, 0, 3, 3, 3) at rotation(90, 0, 90),
                // right_leg texOffs(50, 18) box(-16, 6, 0, 3, 3, 3) at rotation(90, 0, 180).
                addVanillaBox(half, Mat4.identity(), new double[]{0, 0, 0}, new double[]{16, 16, 6}, new double[]{16, 16, 6}, texture, 0, 0, PartPose.ZERO);
                addVanillaBox(half, Mat4.identity(), new double[]{0, 6, 0}, new double[]{3, 9, 3}, new double[]{3, 3, 3}, texture, 50, 6, PartPose.rotation(Math.PI / 2, 0, Math.PI / 2));
                addVanillaBox(half, Mat4.identity(), new double[]{-16, 6, 0}, new double[]{-13, 9, 3}, new double[]{3, 3, 3}, texture, 50, 18, PartPose.rotation(Math.PI / 2, 0, Math.PI));
            }
            transformQuads(half, Mat4.translation(0, 9, foot ? -16 : 0).multiply(Mat4.rotationX(90)));
            quads.addAll(half);
        }
    }

    /**
     * Vanilla {@code ShulkerBoxSpecialRenderer} with {@code ShulkerBoxModel}: the closed box is the
     * {@code lid} and the {@code base}, both on {@code PartPose.offset(0, 24, 0)} — the part space of
     * the entity models, whose y axis grows downwards and whose 24 is the floor of the block. The
     * renderer then puts the 0..1 block onto the pose:
     * {@code translate(0.5, 0.5, 0.5); scale(0.9995); rotateY(180); scale(1, -1, -1);
     * translate(0, -1, 0)}, which is the translation to the block centre, the half turn of the closed
     * box and the flip into the item space used by every other special model. The client shrink of
     * 0.9995 only keeps the lid from z-fighting its own block, so it is left out here.
     *
     * <p>The two boxes overlap by four units: the bottom band of the lid keeps the eight middle texels
     * of every side face transparent and the top band of the base shows through it, which is the
     * clasp of the vanilla texture. The {@code texture} parameter already names the dye colour of the
     * box — {@code entity/shulker/shulker} for the undyed one and {@code entity/shulker/shulker_red}
     * for a red shulker box — so it is used as the sprite path directly.</p>
     */
    private void addShulkerBoxGeometry(List<Quad> quads, SpecialItem special) throws IOException {
        String texture = textureName(normalizeID(special.parameter("texture")));
        BufferedImage skin = resources.texture("minecraft:entity/shulker/" + (texture.isBlank() ? "shulker" : texture));
        PartPose floor = PartPose.offset(0, 24, 0);
        // lid texOffs(0, 0) box(-8, -16, -8, 16, 12, 16) and
        // base texOffs(0, 28) box(-8, -8, -8, 16, 8, 16).
        addVanillaBox(quads, Mat4.identity(), new double[]{-8, -16, -8}, new double[]{8, -4, 8},
                new double[]{16, 12, 16}, skin, 0, 0, floor);
        addVanillaBox(quads, Mat4.identity(), new double[]{-8, -8, -8}, new double[]{8, 0, 8},
                new double[]{16, 8, 16}, skin, 0, 28, floor);
        transformQuads(quads, Mat4.translation(8, 8, 8)
                .multiply(Mat4.rotationY(180))
                .multiply(Mat4.scaling(1, -1, -1))
                .multiply(Mat4.translation(0, -16, 0)));
    }

    /**
     * Vanilla {@code ijz} (ConduitSpecialRenderer) with the {@code hdf.al} shell of {@code hrq}:
     * {@code translate(0.5, 0.5, 0.5)} around the 6³ shell on the 32x16
     * {@code entity/conduit/base} texture. An item is never an active conduit, so the cage, wind and
     * eye layers of the block entity renderer are not part of the icon.
     */
    private void addConduitGeometry(List<Quad> quads) throws IOException {
        // hrq.f(): shell texOffs(0, 0) box(-3, -3, -3, 6, 6, 6) at PartPose.ZERO.
        addVanillaBox(quads, Mat4.identity(), new double[]{-3, -3, -3}, new double[]{3, 3, 3}, new double[]{6, 6, 6},
                resources.texture("minecraft:entity/conduit/base"), 0, 0, PartPose.ZERO);
        transformQuads(quads, Mat4.translation(8, 8, 8));
    }

    /**
     * Vanilla {@code ikl} (TridentSpecialRenderer) with {@code hhg} (TridentModel):
     * {@code scale(1, -1, -1)} around the 32x32 {@code entity/trident} texture. The GUI context of the
     * trident item selects the flat {@code item/trident} model instead, so this is the held pose.
     */
    private void addTridentGeometry(List<Quad> quads) throws IOException {
        BufferedImage texture = resources.texture("minecraft:entity/trident");
        // hhg.a(): pole texOffs(0, 6) box(-0.5, 2, -0.5, 1, 25, 1) with the base texOffs(4, 0)
        // box(-1.5, 0, -0.5, 3, 2, 1), the left spike texOffs(4, 3) box(-2.5, -3, -0.5, 1, 4, 1),
        // the middle spike texOffs(0, 0) box(-0.5, -4, -0.5, 1, 4, 1) and the mirrored right spike
        // texOffs(4, 3) box(1.5, -3, -0.5, 1, 4, 1), all at PartPose.ZERO.
        Mat4 root = Mat4.identity();
        addVanillaBox(quads, root, new double[]{-0.5, 2, -0.5}, new double[]{0.5, 27, 0.5}, new double[]{1, 25, 1}, texture, 0, 6, PartPose.ZERO);
        addVanillaBox(quads, root, new double[]{-1.5, 0, -0.5}, new double[]{1.5, 2, 0.5}, new double[]{3, 2, 1}, texture, 4, 0, PartPose.ZERO);
        addVanillaBox(quads, root, new double[]{-2.5, -3, -0.5}, new double[]{-1.5, 1, 0.5}, new double[]{1, 4, 1}, texture, 4, 3, PartPose.ZERO);
        addVanillaBox(quads, root, new double[]{-0.5, -4, -0.5}, new double[]{0.5, 0, 0.5}, new double[]{1, 4, 1}, texture, 0, 0, PartPose.ZERO);
        addVanillaBox(quads, root, new double[]{1.5, -3, -0.5}, new double[]{2.5, 1, 0.5}, new double[]{1, 4, 1}, texture, 4, 3, 0, true, PartPose.ZERO, ALL_FACES);
        transformQuads(quads, Mat4.scaling(1, -1, -1));
    }

    /**
     * Vanilla {@code ikh} (the {@code head} special renderer) with {@code hsa} (SkullRenderer):
     * {@code translate(0.5, 0, 0.5); scale(-1, -1, 1)} and a 180 degree y rotation of the head part, so
     * the face of the skull looks at the viewer.
     */
    private void addHeadGeometry(List<Quad> quads, SpecialItem special, Map<String, Object> item) throws IOException {
        String kind = textureName(normalizeID(special.parameter("kind")));
        String override = special.parameter("texture");
        List<Quad> parts = new ArrayList<>();
        switch (kind) {
            case "skeleton" -> addSkullHead(parts, headTexture(override, "skeleton/skeleton"), 64, 32, false);
            case "wither_skeleton" -> addSkullHead(parts, headTexture(override, "skeleton/wither_skeleton"), 64, 32, false);
            case "creeper" -> addSkullHead(parts, headTexture(override, "creeper/creeper"), 64, 32, false);
            case "zombie" -> addSkullHead(parts, headTexture(override, "zombie/zombie"), 64, 64, true);
            case "dragon" -> addDragonHead(parts, headTexture(override, "enderdragon/dragon"));
            case "piglin" -> addPiglinHead(parts, headTexture(override, "piglin/piglin"));
            default -> throw new IOException("unsupported head kind: " + kind);
        }
        transformQuads(parts, Mat4.translation(8, 0, 8).multiply(Mat4.scaling(-1, -1, 1)));
        quads.addAll(parts);
    }

    /**
     * Vanilla {@code ike} (the {@code player_head} special renderer): the same skull renderer with the
     * humanoid head layer and the skin of the profile of the item, or the default skin of the client.
     */
    private void addPlayerHeadGeometry(List<Quad> quads, Map<String, Object> item) throws IOException {
        List<Quad> parts = new ArrayList<>();
        addSkullHead(parts, specialSkin(item), 64, 64, true);
        transformQuads(parts, Mat4.translation(8, 0, 8).multiply(Mat4.scaling(-1, -1, 1)));
        quads.addAll(parts);
    }

    /**
     * Vanilla {@code hhl.a()} (the head cube) and {@code hhl.e()} (the humanoid head with the 0.25
     * inflated hat overlay): {@code head} texOffs(0, 0) box(-4, -8, -4, 8, 8, 8) at PartPose.ZERO with
     * the hat as its child at texOffs(32, 0), both turned 180 degrees by {@code SkullModel#setupAnim}.
     */
    private void addSkullHead(List<Quad> quads, BufferedImage texture, int width, int height, boolean humanoid) {
        Mat4 root = PartPose.rotation(0, Math.PI, 0).matrix();
        addVanillaBox(quads, root, new double[]{-4, -8, -4}, new double[]{4, 0, 4}, new double[]{8, 8, 8}, texture, 0, 0, PartPose.ZERO);
        if (humanoid) {
            addVanillaBox(quads, root, new double[]{-4, -8, -4}, new double[]{4, 0, 4}, new double[]{8, 8, 8},
                    texture, 32, 0, 0.25, false, PartPose.ZERO, ALL_FACES);
        }
    }

    /**
     * Vanilla {@code hhj} (DragonHeadModel): the head is scaled to 0.75 and lowered, the jaw hangs on a
     * PartPose of its own and {@code hhj#setupAnim} opens it by 0.2 radians for the idle animation of
     * the icon. The nostril and scale boxes sit on the transparent corner of the dragon texture, which
     * is why the client keeps them.
     */
    private void addDragonHead(List<Quad> quads, BufferedImage texture) throws IOException {
        Mat4 root = PartPose.offset(0, -7.986666, 0).scaled(0.75).matrix();
        Mat4 head = root.multiply(PartPose.rotation(0, Math.PI, 0).matrix());
        // upper_lip texOffs(176, 44) box(-6, -1, -24, 12, 5, 16),
        // upper_head texOffs(112, 30) box(-8, -8, -10, 16, 16, 16),
        // nostril texOffs(112, 0) box(-5, -3, -22, 2, 2, 4) mirrored and box(3, -3, -22, 2, 2, 4),
        // scale texOffs(0, 0) box(-5, -12, -4, 2, 4, 6) mirrored and box(3, -12, -4, 2, 4, 6).
        addVanillaBox(quads, head, new double[]{-8, -8, -10}, new double[]{8, 8, 6}, new double[]{16, 16, 16}, texture, 112, 30, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-6, -1, -24}, new double[]{6, 4, -8}, new double[]{12, 5, 16}, texture, 176, 44, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-5, -3, -22}, new double[]{-3, -1, -18}, new double[]{2, 2, 4}, texture, 112, 0, 0, true, PartPose.ZERO, ALL_FACES);
        addVanillaBox(quads, head, new double[]{3, -3, -22}, new double[]{5, -1, -18}, new double[]{2, 2, 4}, texture, 112, 0, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-5, -12, -4}, new double[]{-3, -8, 2}, new double[]{2, 4, 6}, texture, 0, 0, 0, true, PartPose.ZERO, ALL_FACES);
        addVanillaBox(quads, head, new double[]{3, -12, -4}, new double[]{5, -8, 2}, new double[]{2, 4, 6}, texture, 0, 0, PartPose.ZERO);
        // jaw texOffs(176, 65) box(-6, 0, -16, 12, 4, 16) at offset(0, 4, -8), opened by the idle pose.
        addVanillaBox(quads, head, new double[]{-6, 0, -16}, new double[]{6, 4, 0}, new double[]{12, 4, 16},
                texture, 176, 65, 0, false, PartPose.offsetAndRotation(0, 4, -8, 0.2, 0, 0), ALL_FACES);
    }

    /**
     * Vanilla {@code hev.a} (the piglin head) with the ear angles {@code hhk#setupAnim} applies for the
     * idle animation of the icon: head texOffs(0, 0) box(-5, -8, -4, 10, 8, 8) with the snout, the ear
     * parts at rotation(-PI/6) and rotation(PI/6) turned to -0.7 and 0.7 radians.
     */
    private void addPiglinHead(List<Quad> quads, BufferedImage texture) {
        Mat4 head = PartPose.rotation(0, Math.PI, 0).matrix();
        addVanillaBox(quads, head, new double[]{-5, -8, -4}, new double[]{5, 0, 4}, new double[]{10, 8, 8}, texture, 0, 0, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-2, -4, -5}, new double[]{2, 0, -4}, new double[]{4, 4, 1}, texture, 31, 1, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{2, -2, -5}, new double[]{3, 0, -4}, new double[]{1, 2, 1}, texture, 2, 4, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-3, -2, -5}, new double[]{-2, 0, -4}, new double[]{1, 2, 1}, texture, 2, 0, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{0, 0, -2}, new double[]{1, 5, 2}, new double[]{1, 5, 4}, texture, 51, 6,
                0, false, PartPose.offsetAndRotation(4.5, -6, 0, 0, 0, -0.7), ALL_FACES);
        addVanillaBox(quads, head, new double[]{-1, 0, -2}, new double[]{0, 5, 2}, new double[]{1, 5, 4}, texture, 39, 6,
                0, false, PartPose.offsetAndRotation(-4.5, -6, 0, 0, 0, 0.7), ALL_FACES);
    }

    /**
     * Vanilla {@code ikf} (ShieldSpecialRenderer) with {@code hha} (ShieldModel): {@code scale(1, -1,
     * -1)}. The handle samples {@code entity/shield_base} or {@code entity/shield_base_nopattern}; the
     * plate is drawn with the same sprite and, when the item carries patterns, once more per colour
     * layer against {@code entity/shield/base} and {@code entity/shield/<pattern>}.
     */
    private void addShieldGeometry(List<Quad> quads, Map<String, Object> item) throws IOException {
        List<PatternLayer> patterns = bannerPatterns(item, "entity/shield");
        BufferedImage texture = resources.texture(patterns.isEmpty()
                ? "minecraft:entity/shield_base_nopattern" : "minecraft:entity/shield_base");
        // hdf.dd = shield: plate texOffs(0, 0) box(-6, -11, -2, 12, 22, 1) and
        // handle texOffs(26, 0) box(-1, -3, -1, 2, 6, 6), both at PartPose.ZERO.
        double[] plateFrom = {-6, -11, -2};
        double[] plateTo = {6, 11, -1};
        double[] plateSize = {12, 22, 1};
        List<BufferedImage> plateLayers = new ArrayList<>();
        plateLayers.add(texture);
        if (!patterns.isEmpty()) {
            plateLayers.add(dye(resources.texture("minecraft:entity/shield/base"), bannerColor(null, item)));
            for (PatternLayer layer : patterns) {
                plateLayers.add(dye(resources.texture("minecraft:entity/shield/" + layer.name()), layer.color()));
            }
        }
        addVanillaBox(quads, Mat4.identity(), new double[]{-1, -3, -1}, new double[]{1, 3, 5}, new double[]{2, 6, 6}, texture, 26, 0, PartPose.ZERO);
        for (BufferedImage layer : plateLayers) {
            addVanillaBox(quads, Mat4.identity(), plateFrom, plateTo, plateSize, layer, 0, 0, PartPose.ZERO);
        }
        transformQuads(quads, Mat4.scaling(1, -1, -1));
    }

    /**
     * Vanilla {@code hrs} (DecoratedPotRenderer) with the {@code hdf.aC} base and {@code hdf.aD} side
     * layers. The special renderer adds no pose of its own, so the box list is the item space; every
     * side samples its own pottery pattern sprite and falls back to {@code decorated_pot_side}.
     */
    private void addDecoratedPotGeometry(List<Quad> quads, Map<String, Object> item) throws IOException {
        BufferedImage base = resources.texture("minecraft:entity/decorated_pot/decorated_pot_base");
        // hrs.a(): neck texOffs(0, 0) box(4, 17, 4, 8, 3, 8) inset by 0.1 and texOffs(0, 5)
        // box(5, 20, 5, 6, 1, 6) grown by 0.2, at PartPose.offsetAndRotation(0, 37, 16, PI, 0, 0);
        // top and bottom are the same texOffs(-14, 13) box(0, 0, 0, 14, 0, 14) at offset(1, 16, 1)
        // and offset(1, 0, 1). Only the face that looks out of the pot is kept.
        Mat4 root = Mat4.identity();
        PartPose neck = PartPose.offsetAndRotation(0, 37, 16, Math.PI, 0, 0);
        double[] roundFrom = {0, 0, 0};
        double[] roundTo = {14, 0, 14};
        double[] roundSize = {14, 0, 14};
        addVanillaBox(quads, root, new double[]{4, 17, 4}, new double[]{12, 20, 12}, new double[]{8, 3, 8}, base, 0, 0, -0.1, false, neck, ALL_FACES);
        addVanillaBox(quads, root, new double[]{5, 20, 5}, new double[]{11, 21, 11}, new double[]{6, 1, 6}, base, 0, 5, 0.2, false, neck, ALL_FACES);
        addVanillaBox(quads, root, roundFrom, roundTo, roundSize, base, -14, 13, 0, false, PartPose.offset(1, 16, 1), Set.of("down"));
        addVanillaBox(quads, root, roundFrom, roundTo, roundSize, base, -14, 13, 0, false, PartPose.offset(1, 0, 1), Set.of("up"));
        // hrs.d(): one 14x16 plane per side at texOffs(1, 0) with the north face skipped, in the
        // vanilla back, left, right, front order.
        List<PatternLayer> sherds = potDecorations(item);
        addPotSide(quads, sherds.get(0), PartPose.offsetAndRotation(15, 16, 1, 0, 0, Math.PI));
        addPotSide(quads, sherds.get(1), PartPose.offsetAndRotation(1, 16, 1, 0, -Math.PI / 2, Math.PI));
        addPotSide(quads, sherds.get(2), PartPose.offsetAndRotation(15, 16, 15, 0, Math.PI / 2, Math.PI));
        addPotSide(quads, sherds.get(3), PartPose.offsetAndRotation(1, 16, 15, Math.PI, 0, 0));
    }

    private void addPotSide(List<Quad> quads, PatternLayer sherd, PartPose pose) throws IOException {
        String sprite = sherd.name().isEmpty()
                ? "minecraft:entity/decorated_pot/decorated_pot_side"
                : "minecraft:entity/decorated_pot/" + sherd.name();
        addVanillaBox(quads, Mat4.identity(), new double[]{0, 0, 0}, new double[]{14, 16, 0}, new double[]{14, 16, 0},
                resources.texture(sprite), 1, 0, 0, false, pose, Set.of("north"));
    }

    /**
     * Vanilla {@code ika} (CopperGolemStatueSpecialRenderer) with the {@code hdf.an} standing layer of
     * {@code hbq} (CopperGolemModel): {@code translate(0.5, 1.5, 0.5); scale(-1, -1, 1)}, with the root
     * pose {@code hho#setupAnim} applies to the statue — the y offset of the layer is cleared and the
     * model is rolled by half a turn, which the scale of the renderer cancels again. The other three
     * poses the item can select (sitting, running, star) are not transcribed yet and use this one.
     */
    private void addCopperGolemGeometry(List<Quad> quads, SpecialItem special) throws IOException {
        String texture = special.parameter("texture");
        BufferedImage skin = resources.texture(texture.isBlank()
                ? "minecraft:entity/copper_golem/copper_golem" : normalizeID(texture));
        Mat4 root = PartPose.offsetAndRotation(0, 0, 0, 0, 0, Math.PI).matrix();
        Mat4 body = root.multiply(PartPose.offset(0, -5, 0).matrix());
        Mat4 head = body.multiply(PartPose.offset(0, -6, 0).matrix());
        // hbq.e(): body texOffs(0, 15) box(-4, -6, -3, 8, 6, 6); head texOffs(0, 0)
        // box(-4, -5, -5, 8, 5, 10) grown by 0.015 with the snout texOffs(56, 0) box(-1, -2, -6, 2, 3, 2)
        // and the antenna texOffs(37, 8) and texOffs(37, 0) inset by 0.015; right_arm texOffs(36, 16)
        // box(-3, -1, -2, 3, 10, 4) and left_arm texOffs(50, 16) box(0, -1, -2, 3, 10, 4);
        // right_leg texOffs(0, 27) box(-4, 0, -2, 4, 5, 4) and left_leg texOffs(16, 27) box(0, 0, -2, 4, 5, 4).
        addVanillaBox(quads, body, new double[]{-4, -6, -3}, new double[]{4, 0, 3}, new double[]{8, 6, 6}, skin, 0, 15, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-4, -5, -5}, new double[]{4, 0, 5}, new double[]{8, 5, 10}, skin, 0, 0, 0.015, false, PartPose.ZERO, ALL_FACES);
        addVanillaBox(quads, head, new double[]{-1, -2, -6}, new double[]{1, 1, -4}, new double[]{2, 3, 2}, skin, 56, 0, PartPose.ZERO);
        addVanillaBox(quads, head, new double[]{-1, -9, -1}, new double[]{1, -5, 1}, new double[]{2, 4, 2}, skin, 37, 8, -0.015, false, PartPose.ZERO, ALL_FACES);
        addVanillaBox(quads, head, new double[]{-2, -13, -2}, new double[]{2, -9, 2}, new double[]{4, 4, 4}, skin, 37, 0, -0.015, false, PartPose.ZERO, ALL_FACES);
        addVanillaBox(quads, body, new double[]{-3, -1, -2}, new double[]{0, 9, 2}, new double[]{3, 10, 4}, skin, 36, 16, 0, false, PartPose.offset(-4, -6, 0), ALL_FACES);
        addVanillaBox(quads, body, new double[]{0, -1, -2}, new double[]{3, 9, 2}, new double[]{3, 10, 4}, skin, 50, 16, 0, false, PartPose.offset(4, -6, 0), ALL_FACES);
        addVanillaBox(quads, root, new double[]{-4, 0, -2}, new double[]{0, 5, 2}, new double[]{4, 5, 4}, skin, 0, 27, 0, false, PartPose.offset(0, -5, 0), ALL_FACES);
        addVanillaBox(quads, root, new double[]{0, 0, -2}, new double[]{4, 5, 2}, new double[]{4, 5, 4}, skin, 16, 27, 0, false, PartPose.offset(0, -5, 0), ALL_FACES);
        transformQuads(quads, Mat4.translation(8, 24, 8).multiply(Mat4.scaling(-1, -1, 1)));
    }

    /** The texture of a head family, either the explicit one of the definition or the vanilla default. */
    private BufferedImage headTexture(String override, String fallback) throws IOException {
        String path = override.isBlank() ? fallback : override;
        path = normalizeID(path);
        int separator = path.indexOf(':');
        path = separator < 0 ? path : path.substring(separator + 1);
        return resources.texture("minecraft:textures/entity/" + path + ".png");
    }

    /**
     * The 64x64 skin of a player head: the inline texture of the {@code minecraft:profile} component,
     * or the default skin of the client when the item carries none. Only an embedded
     * {@code data:image/png;base64} skin URL is read; a remote texture hash is left to the caller.
     */
    private BufferedImage specialSkin(Map<String, Object> item) throws IOException {
        Object profile = itemComponent(item, "profile");
        if (profile instanceof Map<?, ?> raw && castMap(raw).get("properties") instanceof List<?> list) {
            for (Object entry : list) {
                if (!(entry instanceof Map<?, ?> property)) {
                    continue;
                }
                Map<String, Object> value = castMap(property);
                if (!"textures".equalsIgnoreCase(Json.string(value, "name", ""))) {
                    continue;
                }
                BufferedImage skin = profileSkin(Json.string(value, "value", ""));
                if (skin != null) {
                    return skin;
                }
            }
        }
        return resources.texture("minecraft:entity/player/wide/steve");
    }

    /**
     * The skin a {@code minecraft:profile} property points at. The property is the base64 payload of
     * the profile document and only its embedded {@code data:image/png:base64} image is supported, so a
     * profile that names a remote texture hash returns null and the caller falls back to the default.
     */
    private BufferedImage profileSkin(String property) throws IOException {
        if (property.isBlank()) {
            return null;
        }
        Map<String, Object> profile;
        try {
            profile = Json.object(Json.parse(new String(Base64.getDecoder().decode(property), StandardCharsets.UTF_8)), "profile");
        } catch (IllegalArgumentException error) {
            return null;
        }
        if (!(profile.get("textures") instanceof Map<?, ?> textures)) {
            return null;
        }
        Object skin = castMap(textures).get("SKIN");
        if (!(skin instanceof Map<?, ?> entry)) {
            return null;
        }
        String url = Json.string(castMap(entry), "url", "");
        if (!url.startsWith("data:image/png;base64,")) {
            return null;
        }
        Map<String, Object> value = new LinkedHashMap<>();
        value.put("texture", url);
        return skinTexture(value);
    }

    /**
     * The pattern layers of an item in the vanilla order: the {@code banner_patterns} component with
     * the dye colour of every layer, resolved against the atlas of the family.
     */
    private List<PatternLayer> bannerPatterns(Map<String, Object> item, String atlas) throws IOException {
        List<PatternLayer> layers = new ArrayList<>();
        if (itemComponent(item, "banner_patterns") instanceof List<?> patterns) {
            for (Object value : patterns) {
                if (!(value instanceof Map<?, ?> raw)) {
                    continue;
                }
                Map<String, Object> pattern = castMap(raw);
                String name = textureName(normalizeID(Json.string(pattern, "pattern", "")));
                if (name.isBlank()) {
                    continue;
                }
                layers.add(new PatternLayer(name, componentColor(pattern.get("color"), 0xFFFFFF)));
            }
        }
        if (layers.size() > 16) {
            throw new IOException("a " + atlas + " item cannot carry more than 16 patterns");
        }
        return layers;
    }

    /**
     * The base colour of a dyeable item. Vanilla takes it from the {@code color} field of the special
     * model definition — which is where the sixteen coloured banner items carry it — and a
     * {@code base_color} component of the item overrides it.
     */
    private int bannerColor(SpecialItem special, Map<String, Object> item) {
        int fallback = special == null ? 0xFFFFFF : componentColor(special.parameter("color"), 0xFFFFFF);
        return componentColor(itemComponent(item, "base_color"), fallback);
    }

    /** The four pottery patterns of a pot in the vanilla back, left, right, front order. */
    private List<PatternLayer> potDecorations(Map<String, Object> item) {
        List<PatternLayer> sherds = new ArrayList<>(List.of(
                new PatternLayer("", 0xFFFFFF), new PatternLayer("", 0xFFFFFF),
                new PatternLayer("", 0xFFFFFF), new PatternLayer("", 0xFFFFFF)));
        if (itemComponent(item, "pot_decorations") instanceof List<?> values) {
            for (int index = 0; index < 4 && index < values.size(); index++) {
                String id = textureName(normalizeID(String.valueOf(values.get(index))));
                if (id.endsWith("_pottery_sherd")) {
                    sherds.set(index, new PatternLayer(
                            id.substring(0, id.length() - "_pottery_sherd".length()) + "_pottery_pattern", 0xFFFFFF));
                }
            }
        }
        return sherds;
    }

    /**
     * One item component, accepting the namespaced and the plain key and looking both inside the
     * component map and on the item itself.
     */
    private static Object itemComponent(Map<String, Object> item, String name) {
        Object value = item.get(name);
        if (value != null) {
            return value;
        }
        if (item.get("components") instanceof Map<?, ?> components) {
            for (String key : List.of("minecraft:" + name, name)) {
                if (components.containsKey(key)) {
                    return components.get(key);
                }
            }
        }
        return null;
    }

    /** A dye colour of a component: the name of a vanilla dye or an explicit rgb value. */
    private static int componentColor(Object value, int fallback) {
        if (value instanceof Number number) {
            return number.intValue() & 0xFFFFFF;
        }
        if (value instanceof Map<?, ?> map) {
            Object rgb = map.get("rgb");
            return rgb instanceof Number number ? number.intValue() & 0xFFFFFF : fallback;
        }
        if (value == null) {
            return fallback;
        }
        String name = String.valueOf(value).trim().toLowerCase(Locale.ROOT);
        int separator = name.indexOf(':');
        if (separator >= 0) {
            name = name.substring(separator + 1);
        }
        for (int index = 0; index < DYE_NAMES.length; index++) {
            if (DYE_NAMES[index].equals(name)) {
                return DYE_COLORS[index];
            }
        }
        return fallback;
    }



    private String selectItemModel(Object value, Map<String, Object> item) throws IOException {
        if (!(value instanceof Map<?, ?> map)) {
            return "";
        }
        @SuppressWarnings("unchecked")
        Map<String, Object> object = (Map<String, Object>) map;
        String type = Json.string(object, "type", "");
        if (type.equals("minecraft:model") || type.equals("model")) {
            return normalizeID(Json.string(object, "model", ""));
        }
        if (type.equals("minecraft:range_dispatch") || type.equals("range_dispatch")) {
            String property = Json.string(object, "property", "");
            double actual = switch (property) {
                case "minecraft:custom_model_data", "custom_model_data" -> Json.number(item, "customModelData", 0);
                case "minecraft:damage", "damage" -> {
                    double damage = Json.number(item, "damage", 0);
                    double max = Json.number(item, "maxDamage", 0);
                    yield max > 0 ? damage / max : damage;
                }
                default -> 0;
            };
            Object entriesValue = object.get("entries");
            if (entriesValue instanceof List<?> entries) {
                String selected = "";
                double threshold = Double.NEGATIVE_INFINITY;
                for (Object entryValue : entries) {
                    if (!(entryValue instanceof Map<?, ?> entryMap)) {
                        continue;
                    }
                    @SuppressWarnings("unchecked")
                    Map<String, Object> entry = (Map<String, Object>) entryMap;
                    double candidate = Json.number(entry, "threshold", Double.POSITIVE_INFINITY);
                    if (candidate <= actual && candidate >= threshold) {
                        threshold = candidate;
                        selected = selectItemModel(entry.get("model"), item);
                    }
                }
                if (!selected.isBlank()) {
                    return selected;
                }
            }
            return selectItemModel(object.get("fallback"), item);
        }
        if (type.equals("minecraft:condition") || type.equals("condition")) {
            String property = Json.string(object, "property", "");
            boolean condition = switch (property) {
                case "minecraft:using_item", "using_item" -> Json.bool(item, "usingItem", false);
                case "minecraft:broken", "broken" -> Json.bool(item, "broken", false);
                default -> false;
            };
            return selectItemModel(condition ? object.get("on_true") : object.get("on_false"), item);
        }
        return "";
    }

    private String itemDefinitionPath(String id) {
        int separator = id.indexOf(':');
        String namespace = separator < 0 ? "minecraft" : id.substring(0, separator);
        String path = separator < 0 ? id : id.substring(separator + 1);
        return "assets/" + namespace + "/items/" + path + ".json";
    }

    private String itemModelComponent(Map<String, Object> item) {
        Object componentsValue = item.get("components");
        if (componentsValue instanceof Map<?, ?>) {
            @SuppressWarnings("unchecked")
            Map<String, Object> components = (Map<String, Object>) componentsValue;
            for (String key : List.of("minecraft:item_model", "item_model", "model")) {
                Object value = components.get(key);
                if (value instanceof String string && !string.isBlank()) {
                    return normalizeID(string);
                }
            }
        }
        Object customModelData = item.get("customModelData");
        if (customModelData instanceof Number) {
            // The old item override predicate is selected below. The value is
            // deliberately not converted to a path: the model owns that map.
        }
        return "";
    }

    private Model loadModel(String id) throws IOException {
        id = normalizeID(id);
        Model inline = inlineModels.get(id);
        if (inline != null) {
            return inline;
        }
        Model cached = models.get(id);
        if (cached != null) {
            return cached;
        }
        Model model = loadModel(id, new ArrayList<>());
        models.put(id, model);
        return model;
    }

    private Model loadModel(String id, List<String> parents) throws IOException {
        if (parents.contains(id)) {
            throw new IOException("cyclic model parent: " + String.join(" -> ", parents));
        }
        Map<String, Object> raw = resources.json(modelPath(id));
        if (raw == null) {
            throw new IOException("missing item model " + id);
        }
        Model own = Model.from(id, raw);
        String parentID = own.parent;
        Model base = null;
        if (!parentID.isBlank() && !parentID.startsWith("builtin/")) {
            ArrayList<String> chain = new ArrayList<>(parents);
            chain.add(id);
            base = loadModel(normalizeID(parentID), chain);
        }
        Model merged = base == null ? new Model(id) : base.copyFor(id);
        merged.merge(own);
        return merged;
    }

    private String modelPath(String id) {
        int separator = id.indexOf(':');
        String namespace = separator < 0 ? "minecraft" : id.substring(0, separator);
        String path = separator < 0 ? id : id.substring(separator + 1);
        if (!path.startsWith("item/") && !path.startsWith("block/") && !path.startsWith("builtin/")) {
            path = "item/" + path;
        }
        return "assets/" + namespace + "/models/" + path + ".json";
    }

    private BufferedImage renderModel(Model model, int size) throws IOException {
        BufferedImage canvas = new BufferedImage(size, size, BufferedImage.TYPE_INT_ARGB);
        Graphics2D graphics = canvas.createGraphics();
        graphics.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_NEAREST_NEIGHBOR);
        java.awt.geom.AffineTransform originalTransform = graphics.getTransform();
        applyTransform(graphics, model.displayTransform("gui"), size);
        List<Face> faces = new ArrayList<>();
        double scale = size / 22.0;
        double centerX = size / 2.0;
        double centerY = size * 0.72;
        for (Element element : model.elements) {
            for (Map.Entry<String, FaceDefinition> entry : element.faces.entrySet()) {
                FaceDefinition definition = entry.getValue();
                String textureID = model.texture(definition.texture, definition.texture);
                BufferedImage texture = resources.texture(textureID);
                List<Vec3> vertices = faceVertices(element, entry.getKey());
                if (vertices.size() < 3) {
                    continue;
                }
                Path2D polygon = new Path2D.Double();
                for (int i = 0; i < vertices.size(); i++) {
                    Point point = project(vertices.get(i), scale, centerX, centerY);
                    if (i == 0) {
                        polygon.moveTo(point.x, point.y);
                    } else {
                        polygon.lineTo(point.x, point.y);
                    }
                }
                polygon.closePath();
                double depth = vertices.stream().mapToDouble(value -> value.x + value.z - value.y * 0.2).average().orElse(0);
                int[] uv = definition.uv == null ? defaultUV(element, entry.getKey()) : definition.uv;
                faces.add(new Face(polygon, texture, depth, shade(entry.getKey()), definition.tintIndex >= 0, uv, definition.rotation));
            }
        }
        faces.sort(Comparator.comparingDouble(value -> value.depth));
        for (Face face : faces) {
            paintFace(graphics, canvas, face);
        }
        graphics.setTransform(originalTransform);
        graphics.dispose();
        if (isTransparent(canvas)) {
            throw new IOException("item model rendered no visible pixels");
        }
        return canvas;
    }

    private int[] defaultUV(Element element, String direction) {
        double[] from = element.from;
        double[] to = element.to;
        return switch (direction) {
            case "up", "down" -> new int[]{(int) from[0], (int) from[2], (int) to[0], (int) to[2]};
            case "north", "south" -> new int[]{(int) from[0], (int) (16 - to[1]), (int) to[0], (int) (16 - from[1])};
            default -> new int[]{(int) from[2], (int) (16 - to[1]), (int) to[2], (int) (16 - from[1])};
        };
    }

    private void applyTransform(Graphics2D graphics, DisplayTransform transform, int size) {
        if (transform == null) {
            return;
        }
        double scale = size / 16.0;
        graphics.translate(size / 2.0 + transform.translation[0] * scale, size / 2.0 + transform.translation[1] * scale);
        graphics.rotate(Math.toRadians(transform.rotation[2]));
        graphics.scale(transform.scale[0], transform.scale[1]);
        graphics.translate(-size / 2.0, -size / 2.0);
    }

    private void paintFace(Graphics2D graphics, BufferedImage canvas, Face face) {
        Shape oldClip = graphics.getClip();
        graphics.clip(face.polygon);
        java.awt.Rectangle bounds = face.polygon.getBounds();
        if (bounds.width > 0 && bounds.height > 0) {
            int[] uv = face.uv;
            int x0 = Math.max(0, Math.min(face.texture.getWidth() - 1, Math.round(uv[0] * face.texture.getWidth() / 16f)));
            int y0 = Math.max(0, Math.min(face.texture.getHeight() - 1, Math.round(uv[1] * face.texture.getHeight() / 16f)));
            int x1 = Math.max(x0 + 1, Math.min(face.texture.getWidth(), Math.round(uv[2] * face.texture.getWidth() / 16f)));
            int y1 = Math.max(y0 + 1, Math.min(face.texture.getHeight(), Math.round(uv[3] * face.texture.getHeight() / 16f)));
            graphics.drawImage(face.texture, bounds.x, bounds.y, bounds.x + bounds.width, bounds.y + bounds.height, x0, y0, x1, y1, null);
            if (face.tint) {
                graphics.setColor(new Color(255, 255, 255, 42));
                graphics.fill(face.polygon);
            }
            if (face.shade > 0) {
                graphics.setColor(new Color(0, 0, 0, face.shade));
                graphics.fill(face.polygon);
            }
        }
        graphics.setClip(oldClip);
    }

    private List<Vec3> faceVertices(Element element, String direction) {
        double x0 = element.from[0], y0 = element.from[1], z0 = element.from[2];
        double x1 = element.to[0], y1 = element.to[1], z1 = element.to[2];
        return switch (direction) {
            case "north" -> List.of(new Vec3(x1, y1, z0), new Vec3(x0, y1, z0), new Vec3(x0, y0, z0), new Vec3(x1, y0, z0));
            case "south" -> List.of(new Vec3(x0, y1, z1), new Vec3(x1, y1, z1), new Vec3(x1, y0, z1), new Vec3(x0, y0, z1));
            case "west" -> List.of(new Vec3(x0, y1, z0), new Vec3(x0, y1, z1), new Vec3(x0, y0, z1), new Vec3(x0, y0, z0));
            case "east" -> List.of(new Vec3(x1, y1, z1), new Vec3(x1, y1, z0), new Vec3(x1, y0, z0), new Vec3(x1, y0, z1));
            case "up" -> List.of(new Vec3(x0, y1, z0), new Vec3(x0, y1, z1), new Vec3(x1, y1, z1), new Vec3(x1, y1, z0));
            case "down" -> List.of(new Vec3(x0, y0, z1), new Vec3(x0, y0, z0), new Vec3(x1, y0, z0), new Vec3(x1, y0, z1));
            default -> List.of();
        };
    }

    private Point project(Vec3 value, double scale, double centerX, double centerY) {
        double x = centerX + (value.x - value.z) * scale * 0.78;
        double y = centerY - (value.y - 8) * scale * 0.92 + (value.x + value.z - 16) * scale * 0.40;
        return new Point(x, y);
    }

    /**
     * Flattens the generated layers into a square icon. A {@code minecraft:generated} model is exactly
     * one 16x16 plate per layer, so the icon is the layer stack scaled to the requested size and
     * nothing else — that keeps a GUI slot blit pixel perfect at the native 16 px icon size.
     */
    private BufferedImage generatedSprite(List<BufferedImage> textures, int size) {
        BufferedImage canvas = new BufferedImage(size, size, BufferedImage.TYPE_INT_ARGB);
        Graphics2D graphics = canvas.createGraphics();
        graphics.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_NEAREST_NEIGHBOR);
        for (BufferedImage texture : textures) {
            graphics.drawImage(texture, 0, 0, size, size, null);
        }
        graphics.dispose();
        return canvas;
    }

    /**
     * Vanilla tooltip rendering: the background sprite is blitted as a nine slice over the text
     * block expanded by 12 px on every side, every line is 10 px tall with an extra 2 px after the
     * title, and the text is always drawn with a shadow.
     */
    private BufferedImage renderTooltip(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> tooltip = Json.object(request.get("tooltip"), "tooltip");
        Map<String, Object> item = tooltip.get("item") instanceof Map<?, ?> value ? castMap(value) : null;
        boolean advanced = Json.bool(tooltip, "advanced", false);
        List<List<TextRun>> lines = buildTooltipLines(tooltip, item, advanced);
        if (lines.isEmpty()) {
            throw new IOException("tooltip requires a title or at least one line");
        }
        boolean icon = Json.bool(tooltip, "icon", false) && item != null && !Json.string(item, "id", "").isBlank();
        int iconSize = icon ? 72 : 0;
        int left = icon ? iconSize + 18 : TOOLTIP_PADDING;
        int wrapWidth = Math.max(
                MIN_WRAP_WIDTH,
                (requestedWidth <= 0 ? DEFAULT_TOOLTIP_WIDTH : requestedWidth) - left - TOOLTIP_PADDING);
        List<List<TextRun>> wrapped = new ArrayList<>();
        for (List<TextRun> line : lines) {
            wrapped.addAll(wrapStyledLine(line, wrapWidth));
        }
        int contentWidth = 0;
        for (List<TextRun> line : wrapped) {
            contentWidth = Math.max(contentWidth, styledWidth(line));
        }
        int contentHeight = wrapped.size() * TOOLTIP_LINE_HEIGHT
                + (wrapped.size() == 1 ? -TOOLTIP_TITLE_GAP : 0);
        int canvasWidth = Math.max(left, contentWidth + left) + TOOLTIP_PADDING;
        int canvasHeight = requestedHeight > 0 ? requestedHeight : contentHeight + TOOLTIP_PADDING * 2;
        BufferedImage canvas = canvas(canvasWidth, canvasHeight);
        drawNineSlice(canvas, resources.texture(TOOLTIP_BACKGROUND_SPRITE),
                nineSliceBorder(TOOLTIP_BACKGROUND_SPRITE, 9), 0, 0, canvasWidth, canvasHeight);
        if (advanced) {
            drawNineSlice(canvas, resources.texture(TOOLTIP_FRAME_SPRITE),
                    nineSliceBorder(TOOLTIP_FRAME_SPRITE, 10), 0, 0, canvasWidth, canvasHeight);
        }
        if (icon) {
            BufferedImage rendered = renderItem(item, iconSize);
            drawScaled(canvas, rendered, TOOLTIP_PADDING, Math.max(6, (canvasHeight - iconSize) / 2), iconSize, iconSize);
        }
        int y = TOOLTIP_PADDING;
        for (int index = 0; index < wrapped.size(); index++) {
            drawStyledLine(canvas, left, y, wrapped.get(index));
            y += TOOLTIP_LINE_HEIGHT + (index == 0 ? TOOLTIP_TITLE_GAP : 0);
        }
        return canvas;
    }

    /** Renders the nine slice of a GUI sprite with the border declared in its mcmeta file. */
    private void drawNineSlice(BufferedImage destination, BufferedImage source, int[] border, int x, int y, int width, int height) {
        drawNineSlice(destination, source, border[0], border[1], border[2], border[3], x, y, width, height);
    }

    private int[] nineSliceBorder(String sprite, int fallback) throws IOException {
        Map<String, Object> metadata = resources.metadata(sprite);
        if (metadata == null || !(metadata.get("gui") instanceof Map<?, ?> gui)) {
            return new int[]{fallback, fallback, fallback, fallback};
        }
        if (!(gui.get("scaling") instanceof Map<?, ?> rawScaling)) {
            return new int[]{fallback, fallback, fallback, fallback};
        }
        Map<String, Object> scaling = castMap(rawScaling);
        Object border = scaling.get("border");
        if (border instanceof Number number) {
            int value = Math.max(0, number.intValue());
            return new int[]{value, value, value, value};
        }
        if (border instanceof Map<?, ?> rawBorder) {
            Map<String, Object> sides = castMap(rawBorder);
            return new int[]{
                    Math.max(0, Json.integer(sides, "left", fallback)),
                    Math.max(0, Json.integer(sides, "top", fallback)),
                    Math.max(0, Json.integer(sides, "right", fallback)),
                    Math.max(0, Json.integer(sides, "bottom", fallback))};
        }
        return new int[]{fallback, fallback, fallback, fallback};
    }

    // ---------------------------------------------------------------------------------------
    // Tooltip content. The order mirrors ItemStack#getTooltipLines: hover name, advanced item id,
    // lore, enchantments, attribute modifiers, the single value component lines and finally the
    // advanced component dump. Lore is the only free form line source, and it comes from the item:
    // the renderer service has no separate tooltip line list.
    // ---------------------------------------------------------------------------------------

    private List<List<TextRun>> buildTooltipLines(Map<String, Object> tooltip, Map<String, Object> item, boolean advanced) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        String title = Json.string(tooltip, "title", "");
        if (!title.isBlank()) {
            lines.add(parseStyledText(title));
        } else if (item != null) {
            List<TextRun> name = hoverName(item);
            if (name != null) {
                lines.add(name);
            }
        }
        String id = item == null ? "" : normalizeID(Json.string(item, "id", ""));
        if (advanced && !id.isBlank()) {
            lines.add(parseStyledText("\u00A78" + id));
        }
        lines.addAll(loreLines(item));
        lines.addAll(enchantmentLines(item));
        lines.addAll(attributeLines(item));
        lines.addAll(componentLines(item, advanced));
        if (advanced && item != null) {
            lines.addAll(advancedLines(item));
        }
        return lines;
    }

    private List<TextRun> hoverName(Map<String, Object> item) throws IOException {
        int rarity = rarityColor(componentValue(item, "minecraft:rarity"));
        String name = Json.string(item, "name", "");
        if (!name.isBlank()) {
            return parseStyledText(applyDefaultStyle(name, rarity, false));
        }
        Object custom = componentValue(item, "minecraft:custom_name");
        if (custom != null) {
            String text = textComponent(custom);
            if (!text.isBlank()) {
                return parseStyledText(applyDefaultStyle(text, rarity, true));
            }
        }
        String id = normalizeID(Json.string(item, "id", ""));
        if (id.isBlank()) {
            return null;
        }
        String path = itemIdPath(id);
        String translated = translate("item.minecraft." + path, "");
        if (translated.isBlank()) {
            translated = translate("block.minecraft." + path, "");
        }
        return parseStyledText(applyDefaultStyle(translated.isBlank() ? path : translated, rarity, false));
    }

    private List<List<TextRun>> loreLines(Map<String, Object> item) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        if (item == null) {
            return lines;
        }
        List<Object> lore = new ArrayList<>();
        if (item.get("lore") instanceof List<?> values) {
            lore.addAll(values);
        } else if (componentValue(item, "minecraft:lore") instanceof List<?> values) {
            lore.addAll(values);
        }
        for (Object entry : lore) {
            String text = entry instanceof String string ? string : textComponent(entry);
            if (!text.isBlank()) {
                lines.add(parseStyledText(applyDefaultStyle(text, 0xAA00AA, true)));
            }
        }
        return lines;
    }

    private List<List<TextRun>> enchantmentLines(Map<String, Object> item) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        if (item == null) {
            return lines;
        }
        LinkedHashMap<String, Integer> levels = new LinkedHashMap<>();
        for (String key : List.of("minecraft:enchantments", "minecraft:stored_enchantments", "enchantments", "stored_enchantments")) {
            collectEnchantments(componentValue(item, key), levels);
        }
        if (item.get("enchantments") instanceof List<?> values) {
            for (Object entry : values) {
                if (!(entry instanceof String text) || text.isBlank()) {
                    continue;
                }
                String[] parts = text.trim().split("\\s+");
                String name = normalizeID(parts[0]);
                int level = 1;
                if (parts.length > 1) {
                    level = romanOrNumber(parts[1]);
                }
                levels.put(name, level);
            }
        }
        for (Map.Entry<String, Integer> entry : levels.entrySet()) {
            String path = itemIdPath(entry.getKey());
            String name = translate("enchantment.minecraft." + path, path);
            int level = entry.getValue();
            if (level > 1) {
                name = name + " " + translate("enchantment.level." + level, Integer.toString(level));
            }
            lines.add(parseStyledText(applyDefaultStyle(name, 0xAAAAAA, false)));
        }
        return lines;
    }

    private void collectEnchantments(Object value, LinkedHashMap<String, Integer> levels) {
        if (value instanceof Map<?, ?> map) {
            Map<String, Object> object = castMap(map);
            Object nested = object.get("levels");
            if (nested instanceof Map<?, ?> nestedMap) {
                collectEnchantments(nestedMap, levels);
                return;
            }
            for (Map.Entry<String, Object> entry : object.entrySet()) {
                if (entry.getValue() instanceof Number number) {
                    levels.put(normalizeID(entry.getKey()), number.intValue());
                } else if (entry.getValue() instanceof Map<?, ?> levelMap) {
                    levels.put(normalizeID(entry.getKey()), Json.integer(castMap(levelMap), "level", 1));
                }
            }
            return;
        }
        if (value instanceof List<?> list) {
            for (Object entry : list) {
                if (entry instanceof Map<?, ?> raw) {
                    Map<String, Object> object = castMap(raw);
                    String id = Json.string(object, "id", Json.string(object, "enchantment", ""));
                    if (!id.isBlank()) {
                        levels.put(normalizeID(id), Json.integer(object, "level", Json.integer(object, "lvl", 1)));
                    }
                }
            }
        }
    }

    private List<List<TextRun>> attributeLines(Map<String, Object> item) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        Object value = item == null ? null : componentValue(item, "minecraft:attribute_modifiers");
        if (!(value instanceof Map<?, ?> map)) {
            return lines;
        }
        Object modifiers = castMap(map).get("modifiers");
        if (!(modifiers instanceof List<?> list)) {
            return lines;
        }
        LinkedHashMap<String, List<List<TextRun>>> grouped = new LinkedHashMap<>();
        for (Object entry : list) {
            if (!(entry instanceof Map<?, ?> raw)) {
                continue;
            }
            Map<String, Object> modifier = castMap(raw);
            String attribute = normalizeID(Json.string(modifier, "type", Json.string(modifier, "attribute", "")));
            if (attribute.isBlank()) {
                continue;
            }
            String slot = Json.string(modifier, "slot", "any").replace("minecraft:", "");
            String header = LineFormat.slotKey(slot);
            double amount = Json.number(modifier, "amount", 0);
            String operation = Json.string(modifier, "operation", "add_value").replace("minecraft:", "");
            int opIndex = switch (operation) {
                case "add_multiplied_base" -> 1;
                case "add_multiplied_total" -> 2;
                default -> 0;
            };
            String kind;
            double shown;
            if (opIndex != 0) {
                kind = "equals";
                shown = amount * 100;
            } else if (amount >= 0) {
                kind = "plus";
                shown = amount;
            } else {
                kind = "take";
                shown = -amount;
            }
            String attributeName = translate("attribute.name." + itemIdPath(attribute), itemIdPath(attribute));
            String pattern = translate("attribute.modifier." + kind + "." + opIndex, "%s %s");
            String text = String.format(pattern, LineFormat.decimal(shown), attributeName);
            // Vanilla prints one line per modifier, grouped under the slot header.
            grouped.computeIfAbsent(header, key -> new ArrayList<>())
                    .add(parseStyledText(applyDefaultStyle(text, 0x5555FF, false)));
        }
        for (Map.Entry<String, List<List<TextRun>>> entry : grouped.entrySet()) {
            lines.add(parseStyledText(applyDefaultStyle(translate("item.modifiers." + entry.getKey(), ""), 0xAAAAAA, false)));
            lines.addAll(entry.getValue());
        }
        lines.removeIf(List::isEmpty);
        return lines;
    }

    private List<List<TextRun>> componentLines(Map<String, Object> item, boolean advanced) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        if (item == null) {
            return lines;
        }
        if (componentValue(item, "minecraft:unbreakable") != null) {
            lines.add(parseStyledText(applyDefaultStyle(translate("item.unbreakable", "Unbreakable"), 0x5555FF, false)));
        }
        if (componentValue(item, "minecraft:dyed_color") != null) {
            lines.add(parseStyledText(applyDefaultStyle(translate("item.dyed", "Dyed"), 0xAAAAAA, false)));
        }
        if (advanced) {
            double damage = Json.number(item, "damage", 0);
            double maxDamage = Json.number(item, "maxDamage", 0);
            if (damage > 0 && maxDamage > 0) {
                String text = String.format(translate("item.durability", "Durability: %s / %s"),
                        LineFormat.decimal(maxDamage - damage), LineFormat.decimal(maxDamage));
                lines.add(parseStyledText(applyDefaultStyle(text, 0xAAAAAA, false)));
            }
        }
        return lines;
    }

    private List<List<TextRun>> advancedLines(Map<String, Object> item) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        int components = 0;
        if (item.get("components") instanceof Map<?, ?> raw) {
            for (Map.Entry<?, ?> entry : raw.entrySet()) {
                if (!(entry.getKey() instanceof String key)) {
                    continue;
                }
                components++;
                switch (key) {
                    case "minecraft:custom_name", "minecraft:rarity", "minecraft:lore", "minecraft:enchantments",
                         "minecraft:stored_enchantments", "minecraft:attribute_modifiers", "minecraft:item_model",
                         "minecraft:unbreakable", "minecraft:dyed_color" -> {
                    }
                    default -> lines.add(parseStyledText(applyDefaultStyle(
                            key + ": " + Json.write(entry.getValue()), 0x555555, false)));
                }
            }
        }
        if (!Json.string(item, "name", "").isBlank()) {
            components++;
        }
        if (item.get("lore") instanceof List<?>) {
            components++;
        }
        if (item.get("enchantments") instanceof List<?>) {
            components++;
        }
        lines.add(parseStyledText(applyDefaultStyle(String.format(translate("item.components", "%s component(s)"), components), 0x555555, false)));
        if (item.get("nbt") instanceof Map<?, ?> nbt) {
            lines.add(parseStyledText(applyDefaultStyle(String.format(translate("item.nbt_tags", "NBT: %s tag(s)"), nbt.size()), 0x555555, false)));
        }
        return lines;
    }

    private Object componentValue(Map<String, Object> item, String key) {
        if (item.get("components") instanceof Map<?, ?> map) {
            if (map.containsKey(key)) {
                return map.get(key);
            }
            String shortKey = key.replace("minecraft:", "");
            if (map.containsKey(shortKey)) {
                return map.get(shortKey);
            }
        }
        return null;
    }

    /** Reads a text component or a plain string, following the single value shapes the API accepts. */
    private String textComponent(Object value) throws IOException {
        if (value instanceof String string) {
            return string;
        }
        if (value instanceof Map<?, ?> map) {
            Map<String, Object> object = castMap(map);
            String text = Json.string(object, "text", "");
            if (!text.isBlank()) {
                return text;
            }
            String translated = Json.string(object, "translate", "");
            if (!translated.isBlank()) {
                Object fallback = object.get("fallback");
                return fallback instanceof String string ? string : translate(translated, translated);
            }
        }
        return String.valueOf(value);
    }

    private int rarityColor(Object value) {
        String rarity = value == null ? "" : String.valueOf(value).replace("minecraft:", "");
        return switch (rarity) {
            case "uncommon" -> 0xFFFF55;
            case "rare" -> 0x55FFFF;
            case "epic" -> 0xFF55FF;
            default -> 0xFFFFFF;
        };
    }

    private static String applyDefaultStyle(String text, int color, boolean italic) {
        if (text.indexOf('\u00A7') >= 0) {
            return text;
        }
        String code = switch (color) {
            case 0xAA00AA -> "\u00A7" + '5';
            case 0x5555FF -> "\u00A7" + '9';
            case 0x555555 -> "\u00A7" + '8';
            case 0xAAAAAA -> "\u00A7" + '7';
            case 0xFFFF55 -> "\u00A7" + 'e';
            case 0x55FFFF -> "\u00A7" + 'b';
            case 0xFF55FF -> "\u00A7" + 'd';
            default -> "";
        };
        return code + (italic ? "\u00A7o" : "") + text;
    }

    private String translate(String key, String fallback) throws IOException {
        String value = resources.language(key);
        return value.isBlank() ? fallback : value;
    }

    private static int romanOrNumber(String value) {
        if (value.matches("\\d+")) {
            return Integer.parseInt(value);
        }
        int total = 0;
        String upper = value.toUpperCase();
        for (int index = 0; index < upper.length(); index++) {
            int current = switch (upper.charAt(index)) {
                case 'I' -> 1;
                case 'V' -> 5;
                case 'X' -> 10;
                case 'L' -> 50;
                case 'C' -> 100;
                default -> 0;
            };
            int next = index + 1 < upper.length() ? switch (upper.charAt(index + 1)) {
                case 'I' -> 1;
                case 'V' -> 5;
                case 'X' -> 10;
                case 'L' -> 50;
                case 'C' -> 100;
                default -> 0;
            } : 0;
            total += current < next ? -current : current;
        }
        return Math.max(1, total);
    }

    /** Character count wrapping used by the simple fixed size toast layout. */
    private List<String> wrapText(String text, int maxCharacters) {
        ArrayList<String> result = new ArrayList<>();
        StringBuilder line = new StringBuilder();
        for (String word : text.replace('\n', ' ').split("\\s+")) {
            if (word.isBlank()) {
                continue;
            }
            if (line.length() > 0 && line.length() + 1 + word.length() > maxCharacters) {
                result.add(line.toString());
                line.setLength(0);
            }
            if (word.length() > maxCharacters) {
                while (word.length() > maxCharacters) {
                    result.add(word.substring(0, maxCharacters));
                    word = word.substring(maxCharacters);
                }
            }
            if (line.length() > 0) {
                line.append(' ');
            }
            line.append(word);
        }
        if (line.length() > 0) {
            result.add(line.toString());
        }
        return result;
    }

    /**
     * Vanilla {@code Font#split}: break at spaces when possible and hard split any word that is
     * wider than the line limit on its own.
     */
    private List<List<TextRun>> wrapStyledLine(List<TextRun> runs, int maxWidth) throws IOException {
        List<List<TextRun>> lines = new ArrayList<>();
        List<TextRun> current = new ArrayList<>();
        // The word gap of the vanilla splitter is the advance of a space in the loaded font.
        int spaceWidth = font().advance(' ');
        int width = 0;
        for (TextRun run : runs) {
            for (String word : run.text().replace('\n', ' ').split(" ", -1)) {
                if (word.isEmpty()) {
                    continue;
                }
                TextRun stretched = run.withText(word);
                int wordWidth = styledWidth(List.of(stretched));
                if (!current.isEmpty() && width + spaceWidth + wordWidth > maxWidth) {
                    lines.add(current);
                    current = new ArrayList<>();
                    width = 0;
                }
                if (wordWidth > maxWidth) {
                    StringBuilder piece = new StringBuilder();
                    int pieceWidth = 0;
                    for (int index = 0; index < word.length(); ) {
                        int codePoint = word.codePointAt(index);
                        String glyph = new String(Character.toChars(codePoint));
                        int advance = glyphAdvance(glyph) + (run.bold() ? 1 : 0);
                        if (pieceWidth + advance > maxWidth && piece.length() > 0) {
                            current.add(stretched.withText(piece.toString()));
                            lines.add(current);
                            current = new ArrayList<>();
                            piece.setLength(0);
                            pieceWidth = 0;
                            width = 0;
                        }
                        piece.append(glyph);
                        pieceWidth += advance;
                        index += Character.charCount(codePoint);
                    }
                    if (piece.length() > 0) {
                        current.add(stretched.withText(piece.toString()));
                        width += pieceWidth;
                    }
                    continue;
                }
                if (!current.isEmpty()) {
                    width += spaceWidth;
                }
                current.add(stretched);
                width += wordWidth;
            }
        }
        if (!current.isEmpty()) {
            lines.add(current);
        }
        if (lines.isEmpty()) {
            lines.add(new ArrayList<>());
        }
        return lines;
    }

    /** Width of a parsed line in font pixels, with the vanilla one pixel bold offset per glyph. */
    private int styledWidth(List<TextRun> line) throws IOException {
        int width = 0;
        for (TextRun run : line) {
            width += textWidth(run.text(), 1);
            if (run.bold()) {
                width += run.text().codePointCount(0, run.text().length());
            }
        }
        return width;
    }

    /** Width of a text that may carry {@code §}/{@code &} formatting codes, at the requested scale. */
    private int styledWidth(String text, int scale) throws IOException {
        int width = 0;
        for (TextRun run : parseStyledText(text)) {
            width += textWidth(run.text(), scale);
            if (run.bold()) {
                width += scale * run.text().codePointCount(0, run.text().length());
            }
        }
        return width;
    }

    private int glyphAdvance(String glyph) throws IOException {
        return textWidth(glyph, 1);
    }

    /**
     * Draws one tooltip line at the vanilla 8 px font cell.
     */
    private void drawStyledLine(BufferedImage destination, int x, int lineTop, List<TextRun> line) throws IOException {
        drawStyledLine(destination, x, lineTop, line, 1, true);
    }

    /**
     * Draws one styled line: the glyphs start at {@code lineTop}, the baseline sits seven pixels per
     * scale unit below it, and when {@code shadow} is set a one unit offset copy of every glyph is
     * painted behind it. Vanilla container titles and map labels pass {@code false} here, tooltips
     * always pass {@code true}.
     */
    private void drawStyledLine(BufferedImage destination, int x, int lineTop, List<TextRun> line, int scale, boolean shadow) throws IOException {
        MinecraftFont font = font();
        Graphics2D graphics = graphics(destination);
        int baseline = lineTop + MinecraftFont.ASCENT * scale;
        int glyphScale = scale * pixelScale;
        for (int pass = shadow ? 0 : 1; pass < 2; pass++) {
            int offset = pass == 0 ? scale : 0;
            int cursor = x;
            for (TextRun run : line) {
                int color = pass == 0 ? shadowColor(run.color()) : run.color();
                for (int index = 0; index < run.text().length(); ) {
                    int codePoint = run.text().codePointAt(index);
                    index += Character.charCount(codePoint);
                    MinecraftFont.Glyph glyph = font.glyph(codePoint);
                    int top = baseline - glyph.ascent() * scale;
                    int advance = glyph.advance() * scale;
                    glyph.draw(graphics, cursor + offset, top + offset, scale, glyphScale, color, run.italic());
                    if (run.bold()) {
                        glyph.draw(graphics, cursor + offset + scale, top + offset, scale, glyphScale, color, run.italic());
                    }
                    if (pass == 1 && run.underline()) {
                        graphics.setColor(new Color(color & 0xFFFFFF));
                        graphics.fillRect(cursor, baseline + 2 * scale, advance, scale);
                    }
                    if (pass == 1 && run.strikethrough()) {
                        graphics.setColor(new Color(color & 0xFFFFFF));
                        graphics.fillRect(cursor, baseline - 3 * scale, advance, scale);
                    }
                    cursor += advance + (run.bold() ? scale : 0);
                }
            }
        }
        graphics.dispose();
    }

    /**
     * Draws one line of legacy formatted text at the vanilla label geometry: the glyph box starts at
     * {@code y} and every {@code §}/{@code &} code switches the colour or style of the glyphs that
     * follow. Runs without a code of their own use {@code defaultColor}.
     */
    private void drawStyledText(BufferedImage destination, int x, int y, String text, int defaultColor, int scale, boolean shadow) throws IOException {
        drawStyledLine(destination, x, y, parseStyledText(text, defaultColor), scale, shadow);
    }

    /** Vanilla shadows are the base colour scaled by a quarter. */
    private static int shadowColor(int color) {
        return ((color >> 2) & 0x3F3F3F);
    }

    /** Parses styled text with the vanilla default colour of a free standing label. */
    private List<TextRun> parseStyledText(String text) {
        return parseStyledText(text, 0xFFFFFF);
    }

    /**
     * Parses the legacy formatting codes of a text component into styled runs.
     *
     * <p>Vanilla only understands {@code §} (0xA7); {@code &} is accepted here as well because the
     * callers of this service hand over user written text that uses it. A prefix character only
     * counts as a code when a valid code character follows it, so a literal ampersand survives, and a
     * run without a code of its own keeps {@code defaultColor} — which is exactly how vanilla applies
     * the colour argument of {@code GuiGraphics#drawString} to unstyled text.</p>
     *
     * <p>A complete {@code §x§R§R§G§G§B§B} sequence is the arbitrary colour form of the same legacy
     * syntax: it only changes the colour and leaves the style switches alone, like a palette code. It
     * is checked before the single character codes because its marker {@code x} is not one of them,
     * and an incomplete sequence is left in the text unchanged instead of being consumed half way —
     * the codes inside it then apply one by one, exactly as they would without the marker.</p>
     */
    private List<TextRun> parseStyledText(String text, int defaultColor) {
        List<TextRun> runs = new ArrayList<>();
        StringBuilder current = new StringBuilder();
        int color = defaultColor;
        boolean bold = false;
        boolean italic = false;
        boolean underline = false;
        boolean strikethrough = false;
        boolean obfuscated = false;
        Style style = new Style(color, bold, italic, underline, strikethrough, obfuscated);
        int index = 0;
        while (index < text.length()) {
            int codePoint = text.codePointAt(index);
            int hexEnd = hexColorEnd(text, index);
            if (hexEnd >= 0) {
                color = hexRgb(text, index);
                if (current.length() > 0) {
                    runs.add(style.toRun(current.toString()));
                    current.setLength(0);
                }
                style = new Style(color, bold, italic, underline, strikethrough, obfuscated);
                index = hexEnd;
                continue;
            }
            if ((codePoint == 0xA7 || codePoint == '&') && index + 1 < text.length()
                    && isFormatCode(text.charAt(index + 1))) {
                char code = Character.toLowerCase(text.charAt(index + 1));
                if (code >= '0' && code <= '9') {
                    color = LEGACY_COLORS[code - '0'];
                } else if (code >= 'a' && code <= 'f') {
                    color = LEGACY_COLORS[code - 'a' + 10];
                } else {
                    switch (code) {
                        case 'l' -> bold = true;
                        case 'o' -> italic = true;
                        case 'n' -> underline = true;
                        case 'm' -> strikethrough = true;
                        case 'k' -> obfuscated = true;
                        case 'r' -> {
                            color = defaultColor;
                            bold = false;
                            italic = false;
                            underline = false;
                            strikethrough = false;
                            obfuscated = false;
                        }
                        default -> {
                        }
                    }
                }
                if (current.length() > 0) {
                    runs.add(style.toRun(current.toString()));
                    current.setLength(0);
                }
                style = new Style(color, bold, italic, underline, strikethrough, obfuscated);
                index += 2;
                continue;
            }
            current.appendCodePoint(codePoint);
            index += Character.charCount(codePoint);
        }
        if (current.length() > 0) {
            runs.add(style.toRun(current.toString()));
        }
        return runs;
    }

    /** The code characters of the legacy format: the sixteen colours plus the five style switches. */
    private static boolean isFormatCode(char code) {
        char lower = Character.toLowerCase(code);
        return lower >= '0' && lower <= '9' || lower >= 'a' && lower <= 'f' || "lonmkr".indexOf(lower) >= 0;
    }

    /** Whether {@code value} is a prefix character of the legacy format, {@code §} or its {@code &} alias. */
    private static boolean isPrefix(char value) {
        return value == 0xA7 || value == '&';
    }

    /** One of the digits a hexadecimal colour nibble is written with, which is ASCII only. */
    private static boolean isHexNibble(char value) {
        return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F';
    }

    /**
     * The index just past the {@code §x§R§R§G§G§B§B} sequence starting at {@code index}, or -1 when
     * there is no complete one there. A sequence is a prefix, the marker {@code x} and six prefix plus
     * hexadecimal digit pairs; the prefixes may differ from each other, and the digits may be written
     * in either case.
     */
    private static int hexColorEnd(String text, int index) {
        if (index + 1 >= text.length() || !isPrefix(text.charAt(index)) || Character.toLowerCase(text.charAt(index + 1)) != 'x') {
            return -1;
        }
        int cursor = index + 2;
        for (int pair = 0; pair < 6; pair++) {
            if (cursor + 1 >= text.length() || !isPrefix(text.charAt(cursor)) || !isHexNibble(text.charAt(cursor + 1))) {
                return -1;
            }
            cursor += 2;
        }
        return cursor;
    }

    /** The {@code 0xRRGGBB} colour of the sequence {@link #hexColorEnd} has just validated. */
    private static int hexRgb(String text, int index) {
        int rgb = 0;
        for (int channel = 0; channel < 3; channel++) {
            int cursor = index + 2 + channel * 4;
            int high = Character.digit(text.charAt(cursor + 1), 16);
            int low = Character.digit(text.charAt(cursor + 3), 16);
            rgb = rgb << 8 | (high << 4 | low);
        }
        return rgb;
    }

    private record TextRun(String text, int color, boolean bold, boolean italic, boolean underline, boolean strikethrough, boolean obfuscated) {
        TextRun withText(String replacement) {
            return new TextRun(replacement, color, bold, italic, underline, strikethrough, obfuscated);
        }
    }

    private record Style(int color, boolean bold, boolean italic, boolean underline, boolean strikethrough, boolean obfuscated) {
        TextRun toRun(String text) {
            return new TextRun(text, color, bold, italic, underline, strikethrough, obfuscated);
        }
    }

    private static final class LineFormat {
        /**
         * Vanilla {@code ItemAttributeModifiers.ATTRIBUTE_MODIFIER_FORMAT}. The symbols are pinned so
         * the output never depends on the JVM default locale of the machine running the renderer.
         */
        private static final java.text.DecimalFormat DECIMAL =
                new java.text.DecimalFormat("#.##", new java.text.DecimalFormatSymbols(java.util.Locale.ROOT));
        private static final List<String> SLOTS = List.of(
                "any", "hand", "armor", "mainhand", "offhand", "head", "chest", "legs", "feet", "body");

        static synchronized String decimal(double value) {
            return DECIMAL.format(value);
        }

        static String slotKey(String slot) {
            return SLOTS.contains(slot) ? slot : "any";
        }
    }

    /**
     * Vanilla bitmap text. The bundled font reproduces the vanilla 8 px cell exactly at size 12:
     * digits and capitals are five pixels wide with an advance of six, and every glyph occupies the
     * seven pixels above its baseline. A line whose top edge is {@code y} therefore has its ink in
     * {@code [y, y + 7 * scale)}, which is the origin every vanilla GUI label coordinate assumes.
     */
    private void drawText(BufferedImage destination, int x, int y, String text, int color) throws IOException {
        drawText(destination, x, y, text, color, 1, false);
    }

    private void drawText(BufferedImage destination, int x, int y, String text, int color, int scale, boolean shadow) throws IOException {
        MinecraftFont font = font();
        int baseline = y + MinecraftFont.ASCENT * scale;
        int glyphScale = scale * pixelScale;
        int shadowColor = shadowColor(color);
        Graphics2D graphics = graphics(destination);
        int cursor = x;
        for (int position = 0; position < text.length(); ) {
            int codePoint = text.codePointAt(position);
            position += Character.charCount(codePoint);
            // Styled tooltip lines are parsed into runs before drawing, so a lone formatting code can
            // only come from a caller that never intended it to be visible.
            if (codePoint == 0xA7) {
                continue;
            }
            MinecraftFont.Glyph glyph = font.glyph(codePoint);
            int top = baseline - glyph.ascent() * scale;
            if (shadow) {
                glyph.draw(graphics, cursor + scale, top + scale, scale, glyphScale, shadowColor, false);
            }
            glyph.draw(graphics, cursor, top, scale, glyphScale, color, false);
            cursor += glyph.advance() * scale;
        }
        graphics.dispose();
    }

    private int textWidth(String text, int scale) throws IOException {
        MinecraftFont font = font();
        int width = 0;
        for (int position = 0; position < text.length(); ) {
            int codePoint = text.codePointAt(position);
            position += Character.charCount(codePoint);
            if (codePoint == 0xA7) {
                continue;
            }
            width += font.advance(codePoint) * scale;
        }
        return width;
    }

    /**
     * The one font of this renderer: the client's own provider chain ({@code font/default.json}),
     * loaded once per resource context so that every label — tooltip, container title, tab list,
     * map label, toast and item count — is measured and drawn by the same glyphs.
     */
    private MinecraftFont font() throws IOException {
        if (font == null) {
            font = MinecraftFont.load(resources);
        }
        return font;
    }

    private void drawNineSlice(BufferedImage destination, BufferedImage source, int left, int top, int right, int bottom, int x, int y, int width, int height) {
        int sourceWidth = source.getWidth();
        int sourceHeight = source.getHeight();
        int innerSourceWidth = sourceWidth - left - right;
        int innerSourceHeight = sourceHeight - top - bottom;
        int innerWidth = Math.max(0, width - left - right);
        int innerHeight = Math.max(0, height - top - bottom);
        Graphics2D graphics = graphics(destination);
        drawPatch(graphics, source, x, y, left, top, 0, 0, left, top);
        drawPatch(graphics, source, x + width - right, y, right, top, sourceWidth - right, 0, sourceWidth, top);
        drawPatch(graphics, source, x, y + height - bottom, left, bottom, 0, sourceHeight - bottom, left, sourceHeight);
        drawPatch(graphics, source, x + width - right, y + height - bottom, right, bottom, sourceWidth - right, sourceHeight - bottom, sourceWidth, sourceHeight);
        drawPatch(graphics, source, x + left, y, innerWidth, top, left, 0, sourceWidth - right, top);
        drawPatch(graphics, source, x + left, y + height - bottom, innerWidth, bottom, left, sourceHeight - bottom, sourceWidth - right, sourceHeight);
        drawPatch(graphics, source, x, y + top, left, innerHeight, 0, top, left, sourceHeight - bottom);
        drawPatch(graphics, source, x + width - right, y + top, right, innerHeight, sourceWidth - right, top, sourceWidth, sourceHeight - bottom);
        drawPatch(graphics, source, x + left, y + top, innerWidth, innerHeight, left, top, left + innerSourceWidth, top + innerSourceHeight);
        graphics.dispose();
    }

    private void drawPatch(Graphics2D graphics, BufferedImage source, int dx, int dy, int dw, int dh, int sx, int sy, int sx2, int sy2) {
        if (dw <= 0 || dh <= 0 || sx2 <= sx || sy2 <= sy) {
            return;
        }
        graphics.drawImage(source, dx, dy, dx + dw, dy + dh, sx, sy, sx2, sy2, null);
    }

    /**
     * Vanilla container screens. Every screen blits a 176 px wide background, draws its title on the
     * row {@code AbstractContainerScreen} uses and places each menu slot at its content coordinate,
     * which is the top left pixel of the 16 px item sitting inside an 18 px slot.
     */
    private BufferedImage renderContainer(Map<String, Object> request, int requestedWidth, int requestedHeight, String kind) throws IOException {
        Map<String, Object> container = Json.object(request.get("container"), "container");
        if (Json.integer(container, "columns", 9) != 9) {
            throw new IOException("containers require exactly 9 columns");
        }
        ContainerLayout layout = containerLayout(kind, Json.integer(container, "rows", 3));
        BufferedImage panel = resources.texture(layout.texture());
        BufferedImage canvas = canvas(layout.width(), layout.height());
        Graphics2D graphics = graphics(canvas);
        if (layout.rows() > 0) {
            // ContainerScreen#renderBg: the slot band grows with the row count while the 96 px player
            // band below it is always the same slice of generic_54.
            int top = CONTAINER_TOP + layout.rows() * 18;
            drawPatch(graphics, panel, 0, 0, layout.width(), top, 0, 0, layout.width(), top);
            drawPatch(graphics, panel, 0, top, layout.width(), CONTAINER_BOTTOM, 0, CONTAINER_BOTTOM_SOURCE,
                    layout.width(), CONTAINER_BOTTOM_SOURCE + CONTAINER_BOTTOM);
        } else {
            drawPatch(graphics, panel, 0, 0, layout.width(), layout.height(), 0, 0, layout.width(), layout.height());
        }
        graphics.dispose();
        String title = Json.string(container, "title", "");
        if (!title.isBlank()) {
            int x = layout.titleX() >= 0 ? layout.titleX() : (layout.width() - styledWidth(title, 1)) / 2;
            drawStyledText(canvas, x, LABEL_Y, title, LABEL_COLOR, 1, false);
        }
        Object slotsValue = container.get("slots");
        if (!(slotsValue instanceof List<?>)) {
            slotsValue = container.get("items");
        }
        if (slotsValue instanceof List<?> slots) {
            for (Object slotValue : slots) {
                if (!(slotValue instanceof Map<?, ?> value)) {
                    continue;
                }
                Map<String, Object> slot = castMap(value);
                Map<String, Object> item = slotItem(slot);
                if (item == null) {
                    continue;
                }
                int[] position = containerSlot(kind, Json.integer(slot, "slot", -1), layout.rows());
                if (position != null) {
                    drawSlotItem(canvas, item, position[0], position[1]);
                }
            }
        }
        return resize(canvas, requestedWidth, requestedHeight);
    }

    /** The ender chest is a plain three row container: same texture, same colours, same slots. */
    private BufferedImage renderEnderchest(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> container = Json.object(request.get("container"), "container");
        if (Json.integer(container, "rows", 3) != 3) {
            throw new IOException("enderchest supports exactly 3 rows and 9 columns");
        }
        return renderContainer(request, requestedWidth, requestedHeight, "inventory");
    }

    private BufferedImage renderSpecialContainer(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> container = Json.object(request.get("container"), "container");
        String kind = Json.string(container, "container", "").toLowerCase(Locale.ROOT);
        kind = switch (kind) {
            case "hopper" -> "hopper";
            case "furnace", "blast_furnace", "smoker" -> kind;
            case "crafting", "crafting_table" -> "crafting_table";
            case "anvil" -> "anvil";
            case "brewing", "brewing_stand" -> "brewing_stand";
            default -> throw new IOException("unsupported_container: " + kind);
        };
        return renderContainer(request, requestedWidth, requestedHeight, kind);
    }

    /** One vanilla container screen: its background texture, canvas size and title column. */
    private record ContainerLayout(String texture, int rows, int width, int height, int titleX) {}

    /**
     * Layout of every supported container screen, mirroring the decompiled 1.21.11 screens. A
     * positive {@code rows} selects the row dependent generic_54 background used by the chest,
     * ender chest and backpack family; {@code titleX} of -1 centres the title in the frame.
     */
    private static ContainerLayout containerLayout(String kind, int rows) throws IOException {
        return switch (kind) {
            case "inventory", "enderchest" -> {
                if (rows < 1 || rows > 6) {
                    throw new IOException("inventory supports 1-6 rows");
                }
                yield new ContainerLayout("minecraft:gui/container/generic_54", rows,
                        CONTAINER_WIDTH, CONTAINER_TOP + rows * 18 + CONTAINER_BOTTOM, 8);
            }
            case "hopper" -> new ContainerLayout("minecraft:gui/container/hopper", 0, 176, 133, 8);
            case "furnace", "blast_furnace", "smoker", "brewing_stand" ->
                    new ContainerLayout("minecraft:gui/container/" + kind, 0, 176, 166, -1);
            case "crafting_table" -> new ContainerLayout("minecraft:gui/container/crafting_table", 0, 176, 166, 29);
            case "anvil" -> new ContainerLayout("minecraft:gui/container/anvil", 0, 176, 166, 60);
            default -> throw new IOException("unsupported_container: " + kind);
        };
    }

    /**
     * Content coordinate of one menu slot, in the order the vanilla menus declare them: the
     * container's own slots first, then the 27 storage rows and the 9 hotbar slots of the player.
     * Returns null for slots the screen does not have.
     */
    private static int[] containerSlot(String kind, int index, int rows) {
        if (index < 0) {
            return null;
        }
        return switch (kind) {
            case "inventory", "enderchest" -> index < rows * 9
                    ? new int[]{8 + (index % 9) * 18, 18 + (index / 9) * 18}
                    : playerSlot(index - rows * 9, 8, 31 + rows * 18);
            case "hopper" -> index < 5 ? new int[]{44 + index * 18, 20} : playerSlot(index - 5, 8, 51);
            case "furnace", "blast_furnace", "smoker" -> index < 3 ? FURNACE_SLOTS[index] : playerSlot(index - 3, 8, 84);
            case "crafting_table" -> index < 9
                    ? new int[]{30 + (index % 3) * 18, 17 + (index / 3) * 18}
                    : index == 9 ? new int[]{124, 35} : playerSlot(index - 10, 8, 84);
            case "anvil" -> index < 3 ? ANVIL_SLOTS[index] : playerSlot(index - 3, 8, 84);
            case "brewing_stand" -> index < 5 ? BREWING_SLOTS[index] : playerSlot(index - 5, 8, 84);
            default -> null;
        };
    }

    /**
     * One slot of the vanilla player inventory block: 27 storage slots in three rows of nine plus the
     * hotbar nine rows further down, at the offsets ContainerMenu and InventoryMenu use.
     */
    private static int[] playerSlot(int index, int x, int y) {
        if (index < 0 || index >= 36) {
            return null;
        }
        return index < 27
                ? new int[]{x + (index % 9) * 18, y + (index / 9) * 18}
                : new int[]{x + (index - 27) * 18, y + PLAYER_INVENTORY_STRIDE};
    }

    /**
     * Returns the native canvas, or the requested size with an exact nearest neighbour scale. The
     * requested size is a layout size, so both buffers end up at the render scale: a caller that asks
     * for a bigger canvas gets the same layout with more device pixels, not a stretched layout.
     */
    private BufferedImage resize(BufferedImage canvas, int requestedWidth, int requestedHeight) {
        int width = requestedWidth > 0 ? device(requestedWidth) : canvas.getWidth();
        int height = requestedHeight > 0 ? device(requestedHeight) : canvas.getHeight();
        if (width == canvas.getWidth() && height == canvas.getHeight()) {
            return canvas;
        }
        BufferedImage output = new BufferedImage(width, height, BufferedImage.TYPE_INT_ARGB);
        Graphics2D graphics = output.createGraphics();
        graphics.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_NEAREST_NEIGHBOR);
        graphics.drawImage(canvas, 0, 0, width, height, null);
        graphics.dispose();
        return output;
    }

    private BufferedImage renderAchievement(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> achievement = Json.object(request.get("achievement"), "achievement");
        String title = Json.string(achievement, "title", "");
        if (title.isBlank()) {
            throw new IOException("achievement.title is required");
        }
        String frame = Json.string(achievement, "frame", "task").toLowerCase();
        if (!frame.equals("task") && !frame.equals("goal") && !frame.equals("challenge")) {
            throw new IOException("achievement.frame must be task, goal, or challenge");
        }
        Map<String, Object> icon = achievement.get("icon") instanceof Map<?, ?> value ? castMap(value) : null;
        if (icon == null || Json.string(icon, "id", "").isBlank()) {
            throw new IOException("achievement.icon.id is required");
        }
        boolean completed = Json.bool(achievement, "completed", false);
        BufferedImage nativeCanvas = canvas(160, 32);
        BufferedImage background = resources.texture("minecraft:gui/sprites/toast/advancement");
        drawScaled(nativeCanvas, background, 0, 0, 160, 32);
        BufferedImage iconImage = renderItem(icon, 24);
        drawScaled(nativeCanvas, iconImage, 5, 4, 24, 24);
        String suffix = completed ? "obtained" : "unobtained";
        BufferedImage frameImage = resources.texture("minecraft:gui/sprites/advancements/" + frame + "_frame_" + suffix);
        drawScaled(nativeCanvas, frameImage, 4, 3, 26, 26);
        // AdvancementToast draws the title and the wrapped description at x 30, rows 7 and 18, and
        // the whole 160x32 toast is scaled up by the requested output size.
        drawStyledText(nativeCanvas, 30, 7, title, completed ? 0xFFFF55 : 0xFFFFFF, 1, false);
        String description = Json.string(achievement, "description", "");
        if (!description.isBlank()) {
            List<String> lines = wrapText(description, 20);
            if (!lines.isEmpty()) {
                drawStyledText(nativeCanvas, 30, 18, lines.get(0), 0xFFFFFF, 1, false);
            }
        }
        int outputWidth = requestedWidth > 0 ? requestedWidth : 480;
        int outputHeight = requestedHeight > 0 ? requestedHeight : 96;
        BufferedImage output = canvas(outputWidth, outputHeight);
        drawScaled(output, nativeCanvas, 0, 0, outputWidth, outputHeight);
        return output;
    }

    private BufferedImage renderMap(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> map = Json.object(request.get("map"), "map");
        BufferedImage raster = mapRaster(map);
        int width = requestedWidth > 0 ? requestedWidth : 140;
        int height = requestedHeight > 0 ? requestedHeight : width;
        if (width < 16 || height < 16) {
            throw new IOException("map dimensions are outside limits");
        }
        BufferedImage canvas = canvas(width, height);
        Graphics2D graphics = graphics(canvas);
        // The vanilla map image is the parchment background with the 128x128 pixel grid inset by
        // the border width of the background texture (1 / 23.3333 of the image).
        graphics.drawImage(resources.texture("minecraft:map/map_background"), 0, 0, width, height, null);
        int insetX = (int) Math.round(width / 23.333333333333332);
        int insetY = (int) Math.round(height / 23.333333333333332);
        int contentWidth = width - insetX * 2;
        int contentHeight = height - insetY * 2;
        graphics.drawImage(raster, insetX, insetY, insetX + contentWidth, insetY + contentHeight, 0, 0, 128, 128, null);
        graphics.dispose();
        Object markersValue = map.get("markers");
        if (markersValue instanceof List<?> markers) {
            for (Object markerValue : markers) {
                if (!(markerValue instanceof Map<?, ?> value)) {
                    continue;
                }
                drawMapMarker(canvas, castMap(value), insetX, insetY, contentWidth, contentHeight, width);
            }
        }
        String dimension = Json.string(map, "dimension", "");
        if (!dimension.isBlank()) {
            drawStyledText(canvas, MAP_LABEL_X, MAP_LABEL_Y, dimension, 0xFFFFFF, 1, false);
        }
        if (Json.bool(map, "locked", false)) {
            drawStyledText(canvas, MAP_LABEL_X, height - MAP_LABEL_BOTTOM, "LOCKED", 0xFFFFFF, 1, false);
        }
        return canvas;
    }

    private BufferedImage mapRaster(Map<String, Object> map) throws IOException {
        Object pixelsValue = map.get("pixels");
        List<?> pixels = pixelsValue instanceof List<?> values ? values : List.of();
        if (pixels.isEmpty() && Json.string(map, "pixelData", "").startsWith("base64:")) {
            try {
                pixels = bytesToNumbers(Base64.getDecoder().decode(Json.string(map, "pixelData", "").substring(7)));
            } catch (IllegalArgumentException error) {
                throw new IOException("invalid map pixelData base64", error);
            }
        } else if (pixels.isEmpty() && !Json.string(map, "pixelData", "").isEmpty()) {
            pixels = bytesToNumbers(Json.string(map, "pixelData", "").getBytes(StandardCharsets.ISO_8859_1));
        }
        if (pixels.size() > 16384) {
            throw new IOException("map pixels cannot exceed 16384 values");
        }
        BufferedImage raster = new BufferedImage(128, 128, BufferedImage.TYPE_INT_ARGB);
        for (int index = 0; index < pixels.size() && index < 16384; index++) {
            if (!(pixels.get(index) instanceof Number number)) {
                continue;
            }
            int x = index % 128;
            int y = index / 128;
            raster.setRGB(x, y, mapColor(number.intValue()));
        }
        return raster;
    }

    private void drawMapMarker(BufferedImage canvas, Map<String, Object> marker, int insetX, int insetY, int contentWidth, int contentHeight, int mapSize) throws IOException {
        int x = Math.max(0, Math.min(127, Json.integer(marker, "x", 0)));
        int z = Math.max(0, Math.min(127, Json.integer(marker, "z", 0)));
        int rotation = Math.floorMod(Json.integer(marker, "rotation", 0), 16);
        BufferedImage icon = mapDecorationTexture(Json.string(marker, "type", ""));
        // Vanilla scales the decoration so that a map of 1120 pixels shows a 96 pixel icon.
        int size = Math.max(4, (int) Math.round(mapSize * 96.0 / 1120.0));
        double centerX = insetX + (x + 0.5) * contentWidth / 128.0;
        double centerY = insetY + (z + 0.5) * contentHeight / 128.0;
        Graphics2D graphics = graphics(canvas);
        AffineTransform original = graphics.getTransform();
        graphics.translate(centerX, centerY);
        graphics.rotate(Math.toRadians(rotation * 22.5));
        graphics.drawImage(icon, -size / 2, -size / 2, size, size, null);
        graphics.setTransform(original);
        graphics.dispose();
        String name = Json.string(marker, "name", "");
        if (!name.isBlank()) {
            drawStyledText(canvas, (int) Math.round(centerX - size / 2.0), (int) Math.round(centerY + size / 2.0), name, 0xFFFFFF, 1, false);
        }
    }

    /** Maps the documented marker names onto the vanilla 1.21.11 map decoration textures. */
    private BufferedImage mapDecorationTexture(String type) throws IOException {
        String name = type == null ? "" : type.trim().toLowerCase(java.util.Locale.ROOT);
        if (name.isEmpty()) {
            name = "frame";
        }
        name = switch (name) {
            case "banner" -> "white_banner";
            case "target" -> "target_point";
            case "village" -> "plains_village";
            case "monument" -> "ocean_monument";
            case "marker" -> "blue_marker";
            default -> name;
        };
        String sprite = "minecraft:map/decorations/" + name;
        if (!resources.hasTexture(sprite)) {
            throw new IOException("unknown map marker type: " + type);
        }
        return resources.texture(sprite);    }

    private List<Integer> bytesToNumbers(byte[] bytes) {
        ArrayList<Integer> values = new ArrayList<>(bytes.length);
        for (byte value : bytes) {
            values.add(value & 0xFF);
        }
        return values;
    }

    private int mapColor(int value) {
        if (value <= 0) {
            return 0;
        }
        int[] bases = {
            0x7FB238, 0xF7E9A3, 0xC7C7C7, 0xFF0000, 0xA0A0FF, 0xA7A7A7,
            0x007C00, 0xFFFFFF, 0xA4A8B8, 0x976D4D, 0x707070, 0x4040FF,
            0x8F7754, 0xFFFDF5, 0xD87F33, 0xB24CD8, 0x6699D8, 0xE5E533,
            0x7FCC19, 0xF27FA5, 0x4C4C4C, 0x999999, 0x4C7F99, 0x7F3FB2,
            0x334CB2, 0x664C33, 0x667F33, 0x993333, 0x191919, 0xFAEE4D,
            0x5CDBD5, 0x4A80FF, 0x00D93A, 0x815631, 0x700200, 0xD1B1A1,
            0x9F5224, 0xBD8B72, 0x1F1F1F, 0x313131, 0xDC0C6C, 0x7B2FBE,
            0x287697, 0xE6E6E6, 0xC2B280, 0xAAB1B8, 0xB02E26, 0xD8AF93,
            0x7D7D7D, 0xB7B7B7, 0xD0D0D0, 0xA0A0A0, 0x707070, 0x505050,
            0x303030, 0x101010, 0x8A8A8A, 0x6B6B6B, 0x4E4E4E, 0x2F2F2F,
            0xE0E0E0, 0xB0B0B0, 0x808080, 0x505050
        };
        int base = bases[Math.min(bases.length - 1, value / 4)];
        double factor = switch (value % 4) {
            case 0 -> 0.53;
            case 1 -> 0.73;
            case 2 -> 0.86;
            default -> 1.0;
        };
        int red = Math.min(255, (int) Math.round(((base >>> 16) & 0xFF) * factor));
        int green = Math.min(255, (int) Math.round(((base >>> 8) & 0xFF) * factor));
        int blue = Math.min(255, (int) Math.round((base & 0xFF) * factor));
        return 0xFF000000 | (red << 16) | (green << 8) | blue;
    }


    private BufferedImage renderPlayerList(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> list = Json.object(request.get("playerList"), "playerList");
        Object playersValue = list.get("players");
        List<Object> players = playersValue instanceof List<?> values ? new ArrayList<>(values) : List.of();
        int columns = Json.integer(list, "columns", 0);
        if (columns == 0) {
            columns = players.size() > 40 ? 3 : players.size() > 20 ? 2 : 1;
        }
        if (columns < 1 || columns > 8) {
            throw new IOException("playerlist columns exceed 8");
        }
        // Vanilla tab list geometry (9 px rows, 8 px faces, 10x8 ping icons) at twice the size, so
        // the font cell scales with the same unit and the rows stay readable.
        int unit = 2;
        int rowHeight = 9 * unit;
        int avatarSize = 8 * unit;
        int pingWidth = 10 * unit;
        int pingHeight = 8 * unit;
        int textRow = rowHeight - avatarSize;
        List<Map<String, Object>> entries = new ArrayList<>();
        int maxText = 0;
        for (Object value : players) {
            if (!(value instanceof Map<?, ?> raw)) {
                continue;
            }
            Map<String, Object> entry = castMap(raw);
            entries.add(entry);
            maxText = Math.max(maxText, styledWidth(playerLabel(entry), unit));
        }
        int rowsPerColumn = Math.max(1, (entries.size() + columns - 1) / columns);
        int rowWidth = avatarSize + unit + maxText + 2 * unit + pingWidth;
        int panelWidth = rowWidth + 2 * unit;
        int panelHeight = rowsPerColumn * rowHeight + 2 * unit;
        List<String> headerLines = splitLines(Json.string(list, "header", ""));
        List<String> footerLines = splitLines(Json.string(list, "footer", ""));
        int headerHeight = headerLines.size() * rowHeight;
        int footerHeight = footerLines.size() * rowHeight;
        int widestText = Math.max(widestLine(headerLines, unit), widestLine(footerLines, unit));
        int width = Math.max(Math.max(requestedWidth, panelWidth * columns), widestText + 4 * unit);
        int height = requestedHeight > 0 ? requestedHeight : panelHeight + headerHeight + footerHeight + 2 * unit;
        BufferedImage canvas = canvas(width, height);
        int panelLeft = (width - panelWidth * columns) / 2;
        int panelTop = headerHeight + Math.max(unit, (height - headerHeight - footerHeight - panelHeight) / 2);
        Graphics2D graphics = graphics(canvas);
        graphics.setColor(new Color(0, 0, 0, 128));
        graphics.fillRect(panelLeft, panelTop, panelWidth * columns, panelHeight);
        graphics.dispose();
        // The header sits directly above the panel and the footer directly below it.
        drawCenteredLines(canvas, headerLines, width, panelTop - headerHeight, rowHeight, unit, textRow);
        for (int index = 0; index < entries.size(); index++) {
            Map<String, Object> entry = entries.get(index);
            int x = panelLeft + (index / rowsPerColumn) * panelWidth + unit;
            int rowTop = panelTop + unit + (index % rowsPerColumn) * rowHeight;
            drawPlayerAvatar(canvas, entry, x, rowTop + textRow / 2, avatarSize);
            drawStyledText(canvas, x + avatarSize + unit, rowTop + textRow / 2, playerLabel(entry), 0xFFFFFF, unit, true);
            drawPingIcon(canvas, Json.integer(entry, "ping", 0), x + rowWidth - pingWidth, rowTop + (rowHeight - pingHeight) / 2, pingWidth, pingHeight);
        }
        drawCenteredLines(canvas, footerLines, width, panelTop + panelHeight, rowHeight, unit, textRow);
        return canvas;
    }

    /** Draws the centred header or footer rows of the tab list, one screen row apart. */
    private void drawCenteredLines(BufferedImage canvas, List<String> lines, int width, int top, int rowHeight, int scale, int textRow) throws IOException {
        for (String line : lines) {
            drawStyledText(canvas, (width - styledWidth(line, scale)) / 2, top + textRow / 2, line, 0xFFFFFF, scale, true);
            top += rowHeight;
        }
    }

    private int widestLine(List<String> lines, int scale) throws IOException {
        int widest = 0;
        for (String line : lines) {
            widest = Math.max(widest, styledWidth(line, scale));
        }
        return widest;
    }

    private static String playerLabel(Map<String, Object> entry) {
        String display = Json.string(entry, "display", "");
        return display.isBlank() ? Json.string(entry, "name", "") : display;
    }

    private static List<String> splitLines(String value) {
        List<String> lines = new ArrayList<>();
        if (value == null || value.isBlank()) {
            return lines;
        }
        for (String line : value.split("\\R")) {
            if (!line.isBlank()) {
                lines.add(line);
            }
        }
        return lines;
    }

    private void drawPlayerAvatar(BufferedImage destination, Map<String, Object> entry, int x, int y, int size) throws IOException {
        String texture = Json.string(entry, "texture", "");
        if (texture.isBlank()) {
            throw new IOException("playerlist entry has no texture: " + playerLabel(entry));
        }
        Map<String, Object> skin = new LinkedHashMap<>();
        skin.put("texture", texture);
        BufferedImage image = skinTexture(skin);
        Graphics2D graphics = graphics(destination);
        drawCrop(graphics, image, x, y, size, size, 8, 8, 8, 8);
        drawCrop(graphics, image, x, y, size, size, 40, 8, 8, 8);
        graphics.dispose();
    }

    private void drawPingIcon(BufferedImage destination, int ping, int x, int y, int width, int height) throws IOException {
        String name = ping <= 0 ? "ping_unknown"
                : ping < 150 ? "ping_5"
                : ping < 300 ? "ping_4"
                : ping < 600 ? "ping_3"
                : ping < 1000 ? "ping_2"
                : "ping_1";
        BufferedImage icon = resources.texture("minecraft:gui/sprites/icon/" + name);
        drawScaled(destination, icon, x, y, width, height);
    }

    /**
     * Vanilla InventoryScreen geometry: the 176x166 container texture is blitted one to one and the
     * player is rendered into the preview viewport that
     * {@code InventoryScreen#renderBg} passes to {@code renderEntityInInventoryFollowsMouse}.
     */
    private BufferedImage renderPlayerInventory(Map<String, Object> request, int requestedWidth, int requestedHeight) throws IOException {
        Map<String, Object> inventory = Json.object(request.get("playerInventory"), "playerInventory");
        Map<String, Object> playerRequest = new LinkedHashMap<>(request);
        playerRequest.put("skin", inventory.get("skin"));
        Map<String, Object> equipment = new LinkedHashMap<>();
        equipment.put("armor", inventory.get("armor"));
        equipment.put("mainHand", inventory.get("mainHand"));
        equipment.put("offhand", inventory.get("offhand"));
        playerRequest.put("equipment", equipment);

        BufferedImage canvas = canvas(INVENTORY_WIDTH, INVENTORY_HEIGHT);
        Graphics2D graphics = graphics(canvas);
        BufferedImage panel = resources.texture("minecraft:gui/container/inventory");
        drawPatch(graphics, panel, 0, 0, INVENTORY_WIDTH, INVENTORY_HEIGHT, 0, 0, INVENTORY_WIDTH, INVENTORY_HEIGHT);
        graphics.dispose();

        Scene scene = new Scene(PLAYER_VIEW_YAW, PLAYER_VIEW_PITCH);
        List<Quad> quads = buildPlayerQuads(playerRequest, scene, 0, 0, false);
        // The preview entity transform of vanilla: translate to the centre of the viewport, scale by
        // 30 screen pixels per block, then place the entity origin at bbHeight / 2 + yOffset.
        scene.scale = INVENTORY_PREVIEW_SCALE / 16.0;
        scene.offsetX = (INVENTORY_PREVIEW_LEFT + INVENTORY_PREVIEW_RIGHT) / 2.0;
        scene.offsetY = (INVENTORY_PREVIEW_TOP + INVENTORY_PREVIEW_BOTTOM) / 2.0
                + (PLAYER_BOUNDING_BOX_HEIGHT / 2 + INVENTORY_PREVIEW_Y_OFFSET) * INVENTORY_PREVIEW_SCALE;
        // The viewport is fixed in layout coordinates, so the model is rasterised at the render
        // scale: a twice as large picture samples the skin at twice the resolution.
        paintScene(canvas, quads, scene, pixelScale);

        String title = Json.string(inventory, "title", "");
        if (!title.isBlank()) {
            // InventoryScreen overrides titleLabelX to 97 so the title clears the recipe book grid.
            drawStyledText(canvas, 97, LABEL_Y, title, LABEL_COLOR, 1, false);
        }
        renderPlayerInventorySlots(canvas, inventory.get("slots"), INVENTORY_BACKPACK_Y);
        Object armorValue = inventory.get("armor");
        if (armorValue instanceof List<?> armor) {
            for (int index = 0; index < armor.size() && index < 4; index++) {
                Map<String, Object> item = slotItem(armor.get(index));
                if (item != null) {
                    drawSlotItem(canvas, item, 8, INVENTORY_ARMOR_Y + index * 18);
                }
            }
        }
        Map<String, Object> offhand = slotItem(inventory.get("offhand"));
        if (offhand != null) {
            drawSlotItem(canvas, offhand, INVENTORY_OFFHAND_X, INVENTORY_OFFHAND_Y);
        }
        return resize(canvas, requestedWidth, requestedHeight);
    }

    /** The 27 storage slots plus the 9 hotbar slots of the player inventory at their menu positions. */
    private void renderPlayerInventorySlots(BufferedImage canvas, Object slotsValue, int y) throws IOException {
        if (!(slotsValue instanceof List<?> slots)) {
            return;
        }
        for (Object slotValue : slots) {
            if (!(slotValue instanceof Map<?, ?> value)) {
                continue;
            }
            Map<String, Object> slot = castMap(value);
            Map<String, Object> item = slotItem(slot);
            if (item == null) {
                continue;
            }
            int[] position = playerSlot(Json.integer(slot, "slot", -1), 8, y);
            if (position != null) {
                drawSlotItem(canvas, item, position[0], position[1]);
            }
        }
    }

    /**
     * Vanilla {@code GuiGraphics#renderItemDecorations}: the 16 px icon is blitted at the slot content
     * position and the stack size sits in the lower right corner of the slot, right aligned two
     * pixels above its border and always drawn with a shadow.
     */
    private void drawSlotItem(BufferedImage destination, Map<String, Object> item, int x, int y) throws IOException {
        try {
            drawScaled(destination, renderItem(item, 16), x, y, 16, 16);
            int count = Json.integer(item, "count", 0);
            if (count > 1) {
                String text = Integer.toString(count);
                drawText(destination, x + 17 - textWidth(text, 1), y + 9, text, 0xFFFFFF, 1, true);
            }
        } catch (RuntimeException error) {
            // renderItem already reports and degrades its own failures; this keeps a slot alive even
            // when the decoration or the canvas blit is what breaks.
            System.err.println("slot item fell back to the missing model: " + error);
            drawScaled(destination, renderMissingItemIcon(device(16)), x, y, 16, 16);
        }
    }

    /** The item of one request slot, or null when the slot is absent, empty or unnamed. */
    private Map<String, Object> slotItem(Object slotValue) {
        if (!(slotValue instanceof Map<?, ?> value)) {
            return null;
        }
        Map<String, Object> slot = castMap(value);
        Map<String, Object> item = slot.get("item") instanceof Map<?, ?> itemValue ? castMap(itemValue) : slot;
        if (Json.bool(slot, "empty", false) || Json.string(item, "id", "").isBlank()) {
            return null;
        }
        return item;
    }

    // ---------------------------------------------------------------------------------------
    // 3D scene pipeline used by the player model.
    //
    // Geometry and texture layout follow the vanilla 1.21.11 PlayerModel / HumanoidModel /
    // EquipmentModel: one unit is one pixel of the 64x64 skin (16 units = one block), x points
    // to the viewer's right, y is up with the feet at 0 and z points towards the viewer.
    // Every face uses the vanilla ModelPart.Cube uv rectangle, so the second skin layer, slim
    // arms, legacy skins and the armour layers sample exactly the same texture regions as the
    // game does.
    // ---------------------------------------------------------------------------------------

    /** Vanilla entity shading: lightAccum = min(1, (max(0, l0*n) + max(0, l1*n)) * 0.6 + 0.4). */
    private static final double[] ENTITY_LIGHT_0 = normalize(new double[]{-0.2, 1.0, -0.7});
    private static final double[] ENTITY_LIGHT_1 = normalize(new double[]{0.2, -1.0, 0.7});

    /**
     * Vanilla tooltip geometry: 12 px of background around the text block, 10 px per text line.
     * The font metrics themselves live in {@link MinecraftFont}: the client's seven pixel ascent,
     * nine pixel line height and per glyph advances are shared by every text surface.
     */
    private static final int DEFAULT_TOOLTIP_WIDTH = 360;
    private static final int TOOLTIP_PADDING = 12;
    private static final int TOOLTIP_LINE_HEIGHT = 10;
    private static final int TOOLTIP_TITLE_GAP = 2;
    /** Vanilla wraps a tooltip at a minimum of four font cells, however narrow the request is. */
    private static final int MIN_WRAP_WIDTH = 48;
    private static final String TOOLTIP_BACKGROUND_SPRITE = "minecraft:gui/sprites/tooltip/background";
    private static final String TOOLTIP_FRAME_SPRITE = "minecraft:gui/sprites/tooltip/frame";

    /** The sixteen legacy chat colours, indexed by their formatting code. */
    private static final int[] LEGACY_COLORS = {
            0x000000, 0x0000AA, 0x00AA00, 0x00AAAA, 0xAA0000, 0xAA00AA, 0xFFAA00, 0xAAAAAA,
            0x555555, 0x5555FF, 0x55FF55, 0x55FFFF, 0xFF5555, 0xFF55FF, 0xFFFF55, 0xFFFFFF
    };

    /**
     * Vanilla container screen geometry in AbstractContainerScreen coordinates. Every screen is
     * 176 px wide; the generic_54 chest background adds 17 px of frame plus one 18 px row per
     * container row on top of the shared 96 px player inventory band.
     */
    private static final int CONTAINER_WIDTH = 176;
    private static final int CONTAINER_TOP = 17;
    private static final int CONTAINER_BOTTOM = 96;
    private static final int CONTAINER_BOTTOM_SOURCE = 126;
    /** AbstractContainerScreen draws every title on row 6 in the 0x404040 label colour. */
    private static final int LABEL_Y = 6;
    private static final int LABEL_COLOR = 0x404040;
    /** The hotbar sits 58 px below the first storage row of every vanilla player inventory. */
    private static final int PLAYER_INVENTORY_STRIDE = 58;

    /** Map labels are inset so the text clears the border of the parchment background. */
    private static final int MAP_LABEL_X = 3;
    private static final int MAP_LABEL_Y = 8;
    private static final int MAP_LABEL_BOTTOM = 11;

    /** Vanilla menu slot coordinates of the screens with a fixed layout, in addSlot order. */
    private static final int[][] FURNACE_SLOTS = {{56, 17}, {56, 53}, {116, 35}};
    private static final int[][] ANVIL_SLOTS = {{27, 47}, {76, 47}, {134, 47}};
    private static final int[][] BREWING_SLOTS = {{56, 51}, {79, 58}, {102, 51}, {79, 17}, {17, 17}};

    /** Vanilla InventoryScreen size and slot geometry (InventoryMenu content coordinates). */
    private static final int INVENTORY_WIDTH = 176;
    private static final int INVENTORY_HEIGHT = 166;
    private static final int INVENTORY_BACKPACK_Y = 84;
    private static final int INVENTORY_ARMOR_Y = 8;
    private static final int INVENTORY_OFFHAND_X = 77;
    private static final int INVENTORY_OFFHAND_Y = 62;
    /**
     * InventoryScreen.renderBg calls
     * {@code renderEntityInInventoryFollowsMouse(g, left + 26, top + 8, left + 75, top + 78, 30, 0.0625F, ...)}
     * and the entity is scaled by 30 screen pixels per block.
     */
    private static final int INVENTORY_PREVIEW_LEFT = 26;
    private static final int INVENTORY_PREVIEW_TOP = 8;
    private static final int INVENTORY_PREVIEW_RIGHT = 75;
    private static final int INVENTORY_PREVIEW_BOTTOM = 78;
    private static final double INVENTORY_PREVIEW_SCALE = 30;
    private static final double INVENTORY_PREVIEW_Y_OFFSET = 0.0625;
    /** Player#getBbHeight() of the standing player used by the preview transform. */
    private static final double PLAYER_BOUNDING_BOX_HEIGHT = 1.8;
    /** Three quarter view shared by every player render, matching the previous 30 / 20 degrees. */
    private static final double PLAYER_VIEW_YAW = 30;
    private static final double PLAYER_VIEW_PITCH = 20;

    private static final double PLAYER_ARM_CLASSIC = 4;
    private static final double PLAYER_ARM_SLIM = 3;
    /**
     * Vanilla HumanoidModel / PlayerModel arm pivots as written in the vanilla entity model space,
     * where y grows downwards from the neck and the model looks along -z. They are consumed by
     * {@link #heldItemTransform} only, which converts the whole vanilla chain through
     * {@link #sceneSpace()} before the result reaches the scene.
     */
    private static final double PLAYER_RIGHT_ARM_PIVOT_X = -5;
    private static final double PLAYER_LEFT_ARM_PIVOT_X = 5;
    private static final double PLAYER_ARM_PIVOT_Y = 2;
    /**
     * The vanilla model space origin — the neck, 24 skin pixels above the feet — expressed in the
     * scene space, whose origin sits at the feet.
     */
    private static final double PLAYER_MODEL_ORIGIN_Y = 24;
    private static final double PLAYER_HEAD_MIN_X = -4;
    private static final double PLAYER_HEAD_MAX_X = 4;
    private static final double PLAYER_HEAD_MIN_Y = 24;
    private static final double PLAYER_HEAD_MAX_Y = 32;
    private static final double PLAYER_HEAD_MIN_Z = -4;
    private static final double PLAYER_HEAD_MAX_Z = 4;
    private static final double PLAYER_BODY_MIN_X = -4;
    private static final double PLAYER_BODY_MAX_X = 4;
    private static final double PLAYER_BODY_MIN_Y = 12;
    private static final double PLAYER_BODY_MAX_Y = 24;
    private static final double PLAYER_BODY_MIN_Z = -2;
    private static final double PLAYER_BODY_MAX_Z = 2;
    private static final double PLAYER_LEG_HALF = 1.9;

    /** One textured quad: corners are stored in texture rectangle order (top-left, top-right, bottom-left, bottom-right). */
    private static final class Quad {
        final Vec3[] corners = new Vec3[4];
        Vec3 normal;
        final BufferedImage texture;
        final double[] uv = new double[4];
        /** Vanilla {@code gui_light: front} models (every generated item model) are drawn unshaded. */
        boolean fullBright;

        Quad(Vec3 normal, BufferedImage texture) {
            this.normal = normal;
            this.texture = texture;
        }
    }

    private static final class Scene {
        final double yaw;
        final double pitch;
        double scale = 1;
        double offsetX;
        double offsetY;

        Scene(double yawDegrees, double pitchDegrees) {
            this.yaw = Math.toRadians(yawDegrees);
            this.pitch = Math.toRadians(pitchDegrees);
        }

        private double[] rotate(double x, double y, double z) {
            double cosYaw = Math.cos(yaw);
            double sinYaw = Math.sin(yaw);
            double x1 = x * cosYaw + z * sinYaw;
            double z1 = -x * sinYaw + z * cosYaw;
            double cosPitch = Math.cos(pitch);
            double sinPitch = Math.sin(pitch);
            return new double[]{x1, y * cosPitch - z1 * sinPitch, y * sinPitch + z1 * cosPitch};
        }

        Point project(Vec3 value) {
            double[] rotated = rotate(value.x, value.y, value.z);
            return new Point(offsetX + rotated[0] * scale, offsetY - rotated[1] * scale);
        }

        double depth(Vec3 value) {
            return rotate(value.x, value.y, value.z)[2];
        }

        Vec3 view(Vec3 value) {
            double[] rotated = rotate(value.x, value.y, value.z);
            return new Vec3(rotated[0], rotated[1], rotated[2]);
        }

        void fit(List<Quad> quads, int width, int height) {
            scale = 1;
            offsetX = 0;
            offsetY = 0;
            double minX = Double.MAX_VALUE;
            double maxX = -Double.MAX_VALUE;
            double minY = Double.MAX_VALUE;
            double maxY = -Double.MAX_VALUE;
            for (Quad quad : quads) {
                for (Vec3 corner : quad.corners) {
                    Point point = project(corner);
                    minX = Math.min(minX, point.x);
                    maxX = Math.max(maxX, point.x);
                    minY = Math.min(minY, point.y);
                    maxY = Math.max(maxY, point.y);
                }
            }
            if (minX > maxX || minY > maxY) {
                return;
            }
            double fitScale = Math.min(width * 0.92 / Math.max(1e-6, maxX - minX), height * 0.92 / Math.max(1e-6, maxY - minY));
            scale = fitScale;
            offsetX = (width - (maxX - minX) * fitScale) / 2.0 - minX * fitScale;
            offsetY = height * 0.95 - maxY * fitScale;
        }
    }

    private void addBoxQuads(List<Quad> quads, double[] from, double[] to, double[] uvSize, BufferedImage texture, int u, int v, boolean mirror) {
        addBoxQuads(quads, from, to, uvSize, texture, u, v, mirror, NO_FACES);
    }

    /**
     * The box layout of the player model, which predates the vanilla cube transcription of
     * {@link #addVanillaBox} and is kept for it: the same six faces in the same texture layout, but
     * with the y and z faces of this renderer's own conventions. {@code skip} drops individual faces.
     */
    private void addBoxQuads(List<Quad> quads, double[] from, double[] to, double[] uvSize, BufferedImage texture, int u, int v, boolean mirror, Set<String> skip) {
        double x0 = from[0];
        double y0 = from[1];
        double z0 = from[2];
        double x1 = to[0];
        double y1 = to[1];
        double z1 = to[2];
        double w = uvSize[0];
        double h = uvSize[1];
        double d = uvSize[2];
        if (mirror) {
            double swap = x0;
            x0 = x1;
            x1 = swap;
        }
        Vec3[] corners = {
            new Vec3(x0, y1, z1), new Vec3(x1, y1, z1), new Vec3(x1, y0, z1), new Vec3(x0, y0, z1),
            new Vec3(x0, y1, z0), new Vec3(x1, y1, z0), new Vec3(x1, y0, z0), new Vec3(x0, y0, z0)
        };
        if (!skip.contains("south")) {
            addQuad(quads, corners, texture, new int[]{0, 1, 3, 2}, new Vec3(0, 0, 1), u + d, v + d, u + d + w, v + d + h);
        }
        if (!skip.contains("north")) {
            addQuad(quads, corners, texture, new int[]{5, 4, 6, 7}, new Vec3(0, 0, -1), u + w + 2 * d, v + d, u + 2 * w + 2 * d, v + d + h);
        }
        if (!skip.contains("west")) {
            addQuad(quads, corners, texture, new int[]{4, 0, 7, 3}, new Vec3(-1, 0, 0), u, v + d, u + d, v + d + h);
        }
        if (!skip.contains("east")) {
            addQuad(quads, corners, texture, new int[]{1, 5, 2, 6}, new Vec3(1, 0, 0), u + w + d, v + d, u + w + 2 * d, v + d + h);
        }
        if (!skip.contains("up")) {
            addQuad(quads, corners, texture, new int[]{4, 5, 0, 1}, new Vec3(0, 1, 0), u + d, v, u + d + w, v + d);
        }
        if (!skip.contains("down")) {
            addQuad(quads, corners, texture, new int[]{3, 2, 7, 6}, new Vec3(0, -1, 0), u + d + w, v, u + 2 * w + d, v + d);
        }
    }

    private void addQuad(List<Quad> quads, Vec3[] corners, BufferedImage texture, int[] order, Vec3 normal, double u0, double v0, double u1, double v1) {
        Quad quad = new Quad(normal, texture);
        for (int index = 0; index < 4; index++) {
            quad.corners[index] = corners[order[index]];
        }
        quad.uv[0] = u0;
        quad.uv[1] = v0;
        quad.uv[2] = u1;
        quad.uv[3] = v1;
        quads.add(quad);
    }


    private static double[] normalize(double[] value) {
        double length = Math.sqrt(value[0] * value[0] + value[1] * value[1] + value[2] * value[2]);
        return new double[]{value[0] / length, value[1] / length, value[2] / length};
    }

    private static double[] inflate(double[] from, double[] to, double amount) {
        return new double[]{from[0] - amount, from[1] - amount, from[2] - amount, to[0] + amount, to[1] + amount, to[2] + amount};
    }

    /**
     * Paints the quads of a scene. {@code pointScale} turns the projected layout coordinates into
     * device pixels, which is what rasterises a 3D model at the resolution of the final picture: the
     * loop below walks device pixels, so a magnified preview samples its skin at that resolution
     * instead of magnifying the pixels sampled at the native one.
     */
    private void paintScene(BufferedImage canvas, List<Quad> quads, Scene scene, double pointScale) {
        // Deterministic software rasteriser: every quad is inverse-mapped to its texture
        // rectangle, so no Java2D clipping or transform rounding can leave seams in the model.
        List<Quad> ordered = new ArrayList<>(quads);
        ordered.sort(Comparator.comparingDouble(quad -> {
            double depth = 0;
            for (Vec3 corner : quad.corners) {
                depth += scene.depth(corner);
            }
            return depth / 4.0;
        }));
        int canvasWidth = canvas.getWidth();
        int canvasHeight = canvas.getHeight();
        int[] target = ((DataBufferInt) canvas.getRaster().getDataBuffer()).getData();
        for (Quad quad : ordered) {
            double u0 = quad.uv[0];
            double v0 = quad.uv[1];
            double du = quad.uv[2] - u0;
            double dv = quad.uv[3] - v0;
            if (Math.abs(du) < 1e-9 || Math.abs(dv) < 1e-9) {
                continue;
            }
            Point[] points = new Point[4];
            for (int index = 0; index < 4; index++) {
                Point point = scene.project(quad.corners[index]);
                points[index] = new Point(point.x * pointScale, point.y * pointScale);
            }
            double ux = (points[1].x - points[0].x) / du;
            double uy = (points[1].y - points[0].y) / du;
            double vx = (points[2].x - points[0].x) / dv;
            double vy = (points[2].y - points[0].y) / dv;
            double originX = points[0].x - ux * u0 - vx * v0;
            double originY = points[0].y - uy * u0 - vy * v0;
            double determinant = ux * vy - vx * uy;
            if (Math.abs(determinant) < 1e-9) {
                continue;
            }
            double uMin = Math.min(quad.uv[0], quad.uv[2]);
            double uMax = Math.max(quad.uv[0], quad.uv[2]);
            double vMin = Math.min(quad.uv[1], quad.uv[3]);
            double vMax = Math.max(quad.uv[1], quad.uv[3]);
            double left = Double.MAX_VALUE;
            double right = -Double.MAX_VALUE;
            double top = Double.MAX_VALUE;
            double bottom = -Double.MAX_VALUE;
            for (Point point : points) {
                left = Math.min(left, point.x);
                right = Math.max(right, point.x);
                top = Math.min(top, point.y);
                bottom = Math.max(bottom, point.y);
            }
            double light = quad.fullBright ? 1.0 : faceBrightness(quad.normal, scene);
            BufferedImage texture = quad.texture;
            int textureWidth = texture.getWidth();
            int textureHeight = texture.getHeight();
            int[] textureData = texture.getRGB(0, 0, textureWidth, textureHeight, null, 0, textureWidth);
            int startX = Math.max(0, (int) Math.floor(left));
            int endX = Math.min(canvasWidth - 1, (int) Math.ceil(right));
            int startY = Math.max(0, (int) Math.floor(top));
            int endY = Math.min(canvasHeight - 1, (int) Math.ceil(bottom));
            for (int y = startY; y <= endY; y++) {
                double sampleY = y + 0.5 - originY;
                for (int x = startX; x <= endX; x++) {
                    double sampleX = x + 0.5 - originX;
                    double u = (sampleX * vy - sampleY * vx) / determinant;
                    if (u < uMin || u >= uMax) {
                        continue;
                    }
                    double v = (sampleY * ux - sampleX * uy) / determinant;
                    if (v < vMin || v >= vMax) {
                        continue;
                    }
                    int texelX = (int) Math.floor(u);
                    int texelY = (int) Math.floor(v);
                    if (texelX < 0 || texelY < 0 || texelX >= textureWidth || texelY >= textureHeight) {
                        continue;
                    }
                    int source = textureData[texelY * textureWidth + texelX];
                    int alpha = source >>> 24;
                    if (alpha == 0) {
                        continue;
                    }
                    if (light < 0.999) {
                        int red = (int) Math.round(((source >> 16) & 0xFF) * light);
                        int green = (int) Math.round(((source >> 8) & 0xFF) * light);
                        int blue = (int) Math.round((source & 0xFF) * light);
                        source = (alpha << 24) | (red << 16) | (green << 8) | blue;
                    }
                    int index = y * canvasWidth + x;
                    target[index] = alpha == 255 ? source : composite(source, target[index]);
                }
            }
        }
    }

    private static int composite(int source, int destination) {
        int sourceAlpha = source >>> 24;
        int destinationAlpha = destination >>> 24;
        int outAlpha = sourceAlpha + destinationAlpha * (255 - sourceAlpha) / 255;
        if (outAlpha == 0) {
            return 0;
        }
        int red = ((source >> 16) & 0xFF) * sourceAlpha + ((destination >> 16) & 0xFF) * destinationAlpha * (255 - sourceAlpha) / 255;
        int green = ((source >> 8) & 0xFF) * sourceAlpha + ((destination >> 8) & 0xFF) * destinationAlpha * (255 - sourceAlpha) / 255;
        int blue = (source & 0xFF) * sourceAlpha + (destination & 0xFF) * destinationAlpha * (255 - sourceAlpha) / 255;
        return (outAlpha << 24) | ((red / outAlpha) << 16) | ((green / outAlpha) << 8) | (blue / outAlpha);
    }


    /**
     * The straight orthographic camera of a vanilla GUI item icon: one unit of the 0..16 item box is
     * {@code size / 16} pixels, the box centre lands on the canvas centre and the viewer looks along
     * {@code -z}. Every 3D orientation of an icon comes from the display transform of its model, not
     * from the camera, which is what makes the icon match the slot render of the client.
     */
    private BufferedImage renderItemIcon(List<Quad> quads, int size) {
        // `size` is already a device size: the item pipeline is the one place where the layout unit
        // and the icon size are the same number, so the scene is projected straight into it.
        BufferedImage canvas = new BufferedImage(size, size, BufferedImage.TYPE_INT_ARGB);
        Scene scene = new Scene(0, 0);
        scene.scale = size / 16.0;
        scene.offsetX = size / 2.0;
        scene.offsetY = size / 2.0;
        paintScene(canvas, quads, scene, 1);
        return canvas;
    }

    /** Moves a list of quads into another space, transforming their corners and their normals. */
    private static void transformQuads(List<Quad> quads, Mat4 transform) {
        for (Quad quad : quads) {
            for (int index = 0; index < 4; index++) {
                double[] point = transform.point(quad.corners[index].x(), quad.corners[index].y(), quad.corners[index].z());
                quad.corners[index] = new Vec3(point[0], point[1], point[2]);
            }
            double[] normal = transform.normal(quad.normal.x(), quad.normal.y(), quad.normal.z());
            quad.normal = new Vec3(normal[0], normal[1], normal[2]);
        }
    }

    /**
     * One box of a vanilla model, transcribed exactly as {@code ModelPart.Cube} bakes it: the six
     * faces in the vanilla texture layout (one unit of depth, then the width, then the mirrored width
     * that closes the top row) and the pose of the part the box belongs to, applied below the matrix of
     * its parents. Vanilla describes the special item models in client code rather than in a model
     * JSON, so the numbers passed here are the {@code addBox} and {@code texOffs} arguments of the
     * decompiled classes, unchanged.
     *
     * <p>{@code faces} is the face set of the vanilla {@code addBox} overload: an empty set means all
     * six faces, and a model that lists a subset — the decorates pot sides list only their north face —
     * emits exactly those. A zero depth box keeps only the two faces of its plane.</p>
     */
    private void addVanillaBox(List<Quad> quads, Mat4 parent, double[] from, double[] to, double[] uvSize,
            BufferedImage texture, int u, int v, double deformation, boolean mirror, PartPose pose, Set<String> faces) {
        double[] box = inflate(from, to, deformation);
        double x0 = box[0];
        double y0 = box[1];
        double z0 = box[2];
        double x1 = box[3];
        double y1 = box[4];
        double z1 = box[5];
        if (mirror) {
            double swap = x0;
            x0 = x1;
            x1 = swap;
        }
        double w = uvSize[0];
        double h = uvSize[1];
        double d = uvSize[2];
        Vec3[] corners = {
            new Vec3(x0, y0, z0), new Vec3(x1, y0, z0), new Vec3(x1, y1, z0), new Vec3(x0, y1, z0),
            new Vec3(x0, y0, z1), new Vec3(x1, y0, z1), new Vec3(x1, y1, z1), new Vec3(x0, y1, z1)
        };
        List<Quad> local = new ArrayList<>();
        // The order of the corners of every face and the rectangle it samples are the vanilla ones,
        // including the inverted v range of the top face.
        if (faces.contains("down")) {
            addQuad(local, corners, texture, new int[]{4, 5, 0, 1}, new Vec3(0, -1, 0), u + d, v, u + d + w, v + d);
        }
        if (faces.contains("up")) {
            addQuad(local, corners, texture, new int[]{3, 2, 7, 6}, new Vec3(0, 1, 0), u + d + w, v + d, u + d + 2 * w, v);
        }
        if (faces.contains("north")) {
            addQuad(local, corners, texture, new int[]{0, 1, 3, 2}, new Vec3(0, 0, -1), u + d, v + d, u + d + w, v + d + h);
        }
        if (faces.contains("south")) {
            addQuad(local, corners, texture, new int[]{5, 4, 6, 7}, new Vec3(0, 0, 1), u + 2 * d + w, v + d, u + 2 * d + 2 * w, v + d + h);
        }
        if (faces.contains("west")) {
            addQuad(local, corners, texture, new int[]{4, 0, 7, 3}, new Vec3(-1, 0, 0), u, v + d, u + d, v + d + h);
        }
        if (faces.contains("east")) {
            addQuad(local, corners, texture, new int[]{1, 5, 2, 6}, new Vec3(1, 0, 0), u + d + w, v + d, u + 2 * d + w, v + d + h);
        }
        transformQuads(local, parent.multiply(pose.matrix()));
        quads.addAll(local);
    }

    /** A box of a model whose part poses are already folded into {@code parent}. */
    private void addVanillaBox(List<Quad> quads, Mat4 parent, double[] from, double[] to, double[] uvSize,
            BufferedImage texture, int u, int v, PartPose pose) {
        addVanillaBox(quads, parent, from, to, uvSize, texture, u, v, 0, false, pose, ALL_FACES);
    }


    private double faceBrightness(Vec3 normal, Scene scene) {
        Vec3 view = scene.view(normal);
        double light0 = Math.max(0, view.x * ENTITY_LIGHT_0[0] + view.y * ENTITY_LIGHT_0[1] + view.z * ENTITY_LIGHT_0[2]);
        double light1 = Math.max(0, view.x * ENTITY_LIGHT_1[0] + view.y * ENTITY_LIGHT_1[1] + view.z * ENTITY_LIGHT_1[2]);
        return Math.min(1.0, (light0 + light1) * 0.6 + 0.4);
    }

    private void addPlayerModel(List<Quad> quads, BufferedImage skin, boolean slim, boolean outer) {
        double arm = slim ? PLAYER_ARM_SLIM : PLAYER_ARM_CLASSIC;
        double[] headFrom = {PLAYER_HEAD_MIN_X, PLAYER_HEAD_MIN_Y, PLAYER_HEAD_MIN_Z};
        double[] headTo = {PLAYER_HEAD_MAX_X, PLAYER_HEAD_MAX_Y, PLAYER_HEAD_MAX_Z};
        double[] bodyFrom = {PLAYER_BODY_MIN_X, PLAYER_BODY_MIN_Y, PLAYER_BODY_MIN_Z};
        double[] bodyTo = {PLAYER_BODY_MAX_X, PLAYER_BODY_MAX_Y, PLAYER_BODY_MAX_Z};
        double[] rightArmFrom = {PLAYER_BODY_MIN_X - arm, PLAYER_BODY_MIN_Y, PLAYER_BODY_MIN_Z};
        double[] rightArmTo = {PLAYER_BODY_MIN_X, PLAYER_BODY_MAX_Y, PLAYER_BODY_MAX_Z};
        double[] leftArmFrom = {PLAYER_BODY_MAX_X, PLAYER_BODY_MIN_Y, PLAYER_BODY_MIN_Z};
        double[] leftArmTo = {PLAYER_BODY_MAX_X + arm, PLAYER_BODY_MAX_Y, PLAYER_BODY_MAX_Z};
        double[] rightLegFrom = {-PLAYER_LEG_HALF - 2, 0, PLAYER_BODY_MIN_Z};
        double[] rightLegTo = {-PLAYER_LEG_HALF + 2, 12, PLAYER_BODY_MAX_Z};
        double[] leftLegFrom = {PLAYER_LEG_HALF - 2, 0, PLAYER_BODY_MIN_Z};
        double[] leftLegTo = {PLAYER_LEG_HALF + 2, 12, PLAYER_BODY_MAX_Z};
        addPlayerPart(quads, skin, 0, 0, 32, 0, headFrom, headTo, new double[]{8, 8, 8}, 0.5, outer);
        addPlayerPart(quads, skin, 16, 16, 16, 32, bodyFrom, bodyTo, new double[]{8, 12, 4}, 0.25, outer);
        addPlayerPart(quads, skin, 40, 16, 40, 32, rightArmFrom, rightArmTo, new double[]{arm, 12, 4}, 0.25, outer);
        addPlayerPart(quads, skin, 32, 48, 48, 48, leftArmFrom, leftArmTo, new double[]{arm, 12, 4}, 0.25, outer);
        addPlayerPart(quads, skin, 0, 16, 0, 32, rightLegFrom, rightLegTo, new double[]{4, 12, 4}, 0.25, outer);
        addPlayerPart(quads, skin, 16, 48, 0, 48, leftLegFrom, leftLegTo, new double[]{4, 12, 4}, 0.25, outer);
    }

    private void addPlayerPart(List<Quad> quads, BufferedImage texture, int u, int v, int layerU, int layerV, double[] from, double[] to, double[] uvSize, double inflation, boolean outer) {
        addBoxQuads(quads, from, to, uvSize, texture, u, v, false);
        if (outer) {
            double[] inflated = inflate(from, to, inflation);
            addBoxQuads(quads, new double[]{inflated[0], inflated[1], inflated[2]}, new double[]{inflated[3], inflated[4], inflated[5]},
                    uvSize, texture, layerU, layerV, false);
        }
    }

    private void addCape(List<Quad> quads, BufferedImage cape) {
        // Vanilla cloak part: 10 wide, 16 tall and 1 thick, hanging just behind the body. Cape
        // textures store the artwork on the outward facing rectangle, so the outward face uses
        // the design region and the mirrored corner order of the vanilla back face.
        double x0 = -5;
        double x1 = 5;
        double y0 = 8;
        double y1 = 24;
        double outer = PLAYER_BODY_MIN_Z - 0.6;
        double inner = PLAYER_BODY_MIN_Z;
        Vec3[] corners = {
            new Vec3(x0, y1, outer), new Vec3(x1, y1, outer), new Vec3(x1, y0, outer), new Vec3(x0, y0, outer),
            new Vec3(x0, y1, inner), new Vec3(x1, y1, inner), new Vec3(x1, y0, inner), new Vec3(x0, y0, inner)
        };
        addQuad(quads, corners, cape, new int[]{1, 0, 2, 3}, new Vec3(0, 0, -1), 1, 1, 11, 17);
        addQuad(quads, corners, cape, new int[]{4, 5, 7, 6}, new Vec3(0, 0, 1), 12, 1, 22, 17);
        addQuad(quads, corners, cape, new int[]{4, 0, 7, 3}, new Vec3(-1, 0, 0), 0, 1, 1, 17);
        addQuad(quads, corners, cape, new int[]{1, 5, 2, 6}, new Vec3(1, 0, 0), 11, 1, 12, 17);
        addQuad(quads, corners, cape, new int[]{4, 5, 0, 1}, new Vec3(0, 1, 0), 1, 0, 11, 1);
        addQuad(quads, corners, cape, new int[]{3, 2, 7, 6}, new Vec3(0, -1, 0), 11, 0, 21, 1);
    }

    private void addElytra(List<Quad> quads, BufferedImage cape) throws IOException {
        BufferedImage source = cape != null ? cape : resources.texture("minecraft:entity/equipment/wings/elytra");
        double unit = Math.max(1, source.getWidth() / 64.0);
        double z = PLAYER_BODY_MIN_Z - 1.4;
        double top = PLAYER_BODY_MAX_Y - 1;
        double bottom = PLAYER_BODY_MIN_Y + 2;
        // The vanilla wings layer samples the wing rectangle of the elytra texture and mirrors
        // the left wing; the player cape texture is used instead when the layer asks for it.
        Vec3[] rightWing = {new Vec3(-0.6, top, z), new Vec3(-4.6, top, z), new Vec3(-0.6, bottom, z), new Vec3(-4.6, bottom, z)};
        Vec3[] leftWing = {new Vec3(4.6, top, z), new Vec3(0.6, top, z), new Vec3(4.6, bottom, z), new Vec3(0.6, bottom, z)};
        for (Vec3[] wing : List.of(rightWing, leftWing)) {
            Quad quad = new Quad(new Vec3(0, 0, -1), source);
            for (int index = 0; index < 4; index++) {
                quad.corners[index] = wing[index];
            }
            quad.uv[0] = 34 * unit;
            quad.uv[1] = 2 * unit;
            quad.uv[2] = 46 * unit;
            quad.uv[3] = 22 * unit;
            quads.add(quad);
        }
    }

    private void addEquipmentLayers(List<Quad> quads, Object equipmentValue, boolean slim, BufferedImage cape) throws IOException {
        if (!(equipmentValue instanceof Map<?, ?> rawEquipment)) {
            return;
        }
        Object armorValue = rawEquipment.get("armor");
        if (!(armorValue instanceof List<?> armor)) {
            return;
        }
        for (int slot = 0; slot < armor.size() && slot < 4; slot++) {
            if (!(armor.get(slot) instanceof Map<?, ?> slotRaw)) {
                continue;
            }
            Map<String, Object> slotValue = castMap(slotRaw);
            Map<String, Object> item = slotValue.get("item") instanceof Map<?, ?> itemRaw ? castMap(itemRaw) : slotValue;
            String id = normalizeID(Json.string(item, "id", ""));
            if (id.isBlank()) {
                continue;
            }
            if (id.equals("minecraft:elytra")) {
                if (slot == 1) {
                    addElytra(quads, cape);
                }
                continue;
            }
            String material = armorMaterial(id);
            if (material == null) {
                throw new IOException("unsupported player armour item: " + id);
            }
            String layerType = slot == 2 ? "humanoid_leggings" : "humanoid";
            Map<String, Object> definition = resources.json("assets/minecraft/equipment/" + material + ".json");
            if (definition == null) {
                throw new IOException("missing equipment definition: " + material);
            }
            Object layersValue = definition.get("layers");
            List<?> layers = layersValue instanceof Map<?, ?> layerMap && layerMap.get(layerType) instanceof List<?> list ? list : List.of();
            if (layers.isEmpty()) {
                throw new IOException("missing equipment layer " + layerType + ": " + material);
            }
            BufferedImage texture = null;
            for (Object layerValue : layers) {
                if (!(layerValue instanceof Map<?, ?> layerRaw)) {
                    continue;
                }
                Map<String, Object> layer = castMap(layerRaw);
                String textureID = Json.string(layer, "texture", material);
                BufferedImage layerTexture = resources.texture("minecraft:entity/equipment/" + layerType + "/" + textureName(textureID));
                if (layer.get("dyeable") instanceof Map<?, ?> dyeable) {
                    int color = dyedColor(item, Json.integer(castMap(dyeable), "color_when_undyed", 0x00A06540));
                    layerTexture = dye(layerTexture, color);
                }
                texture = texture == null ? layerTexture : overlayTexture(texture, layerTexture);
            }
            if (texture != null) {
                addArmorBoxes(quads, texture, slot, slim);
            }
        }
    }

    private String armorMaterial(String id) {
        for (String slot : new String[]{"helmet", "chestplate", "leggings", "boots"}) {
            String suffix = "_" + slot;
            if (id.endsWith(suffix)) {
                String material = id.substring(0, id.length() - suffix.length());
                return material.startsWith("minecraft:") ? material.substring("minecraft:".length()) : material;
            }
        }
        return null;
    }

    private static String textureName(String textureID) {
        int separator = textureID.indexOf(':');
        return separator < 0 ? textureID : textureID.substring(separator + 1);
    }

    private int dyedColor(Map<String, Object> item, int fallback) {
        Object components = item.get("components");
        if (components instanceof Map<?, ?> map) {
            Object dyed = map.containsKey("minecraft:dyed_color") ? map.get("minecraft:dyed_color") : map.get("dyed_color");
            if (dyed instanceof Number number) {
                return number.intValue() & 0xFFFFFF;
            }
            if (dyed instanceof Map<?, ?> dyedMap && dyedMap.get("rgb") instanceof Number rgb) {
                return rgb.intValue() & 0xFFFFFF;
            }
        }
        if (item.get("dyed_color") instanceof Number number) {
            return number.intValue() & 0xFFFFFF;
        }
        return fallback & 0xFFFFFF;
    }

    private BufferedImage dye(BufferedImage texture, int color) {
        BufferedImage tinted = new BufferedImage(texture.getWidth(), texture.getHeight(), BufferedImage.TYPE_INT_ARGB);
        double red = ((color >> 16) & 0xFF) / 255.0;
        double green = ((color >> 8) & 0xFF) / 255.0;
        double blue = (color & 0xFF) / 255.0;
        for (int y = 0; y < texture.getHeight(); y++) {
            for (int x = 0; x < texture.getWidth(); x++) {
                int value = texture.getRGB(x, y);
                int alpha = value >>> 24;
                if (alpha == 0) {
                    continue;
                }
                int outRed = Math.min(255, (int) Math.round(((value >> 16) & 0xFF) * red));
                int outGreen = Math.min(255, (int) Math.round(((value >> 8) & 0xFF) * green));
                int outBlue = Math.min(255, (int) Math.round((value & 0xFF) * blue));
                tinted.setRGB(x, y, (alpha << 24) | (outRed << 16) | (outGreen << 8) | outBlue);
            }
        }
        return tinted;
    }

    private BufferedImage overlayTexture(BufferedImage base, BufferedImage overlay) {
        BufferedImage combined = new BufferedImage(base.getWidth(), base.getHeight(), BufferedImage.TYPE_INT_ARGB);
        Graphics2D graphics = combined.createGraphics();
        graphics.drawImage(base, 0, 0, null);
        graphics.drawImage(overlay, 0, 0, null);
        graphics.dispose();
        return combined;
    }

    private void addArmorBoxes(List<Quad> quads, BufferedImage texture, int slot, boolean slim) {
        double arm = slim ? PLAYER_ARM_SLIM : PLAYER_ARM_CLASSIC;
        double deformation = slot == 2 ? 0.5 : 1.0;
        double[] headFrom = {PLAYER_HEAD_MIN_X, PLAYER_HEAD_MIN_Y, PLAYER_HEAD_MIN_Z};
        double[] headTo = {PLAYER_HEAD_MAX_X, PLAYER_HEAD_MAX_Y, PLAYER_HEAD_MAX_Z};
        double[] bodyFrom = {PLAYER_BODY_MIN_X, PLAYER_BODY_MIN_Y, PLAYER_BODY_MIN_Z};
        double[] bodyTo = {PLAYER_BODY_MAX_X, PLAYER_BODY_MAX_Y, PLAYER_BODY_MAX_Z};
        double[] rightArmFrom = {PLAYER_BODY_MIN_X - arm, PLAYER_BODY_MIN_Y, PLAYER_BODY_MIN_Z};
        double[] rightArmTo = {PLAYER_BODY_MIN_X, PLAYER_BODY_MAX_Y, PLAYER_BODY_MAX_Z};
        double[] leftArmFrom = {PLAYER_BODY_MAX_X, PLAYER_BODY_MIN_Y, PLAYER_BODY_MIN_Z};
        double[] leftArmTo = {PLAYER_BODY_MAX_X + arm, PLAYER_BODY_MAX_Y, PLAYER_BODY_MAX_Z};
        double[] rightLegFrom = {-PLAYER_LEG_HALF - 2, 0, PLAYER_BODY_MIN_Z};
        double[] rightLegTo = {-PLAYER_LEG_HALF + 2, 12, PLAYER_BODY_MAX_Z};
        double[] leftLegFrom = {PLAYER_LEG_HALF - 2, 0, PLAYER_BODY_MIN_Z};
        double[] leftLegTo = {PLAYER_LEG_HALF + 2, 12, PLAYER_BODY_MAX_Z};
        // Vanilla equipment layer part visibility: helmet = head, chestplate = body + arms,
        // leggings = body + legs, boots = legs. The armour texture only stores the right limbs,
        // so the left ones sample the same rectangle mirrored.
        if (slot == 0) {
            addInflatedBox(quads, headFrom, headTo, new double[]{8, 8, 8}, texture, 0, 0, deformation, false);
        } else if (slot == 1) {
            addInflatedBox(quads, bodyFrom, bodyTo, new double[]{8, 12, 4}, texture, 16, 16, deformation, false);
            addInflatedBox(quads, rightArmFrom, rightArmTo, new double[]{arm, 12, 4}, texture, 40, 16, deformation, false);
            addInflatedBox(quads, leftArmFrom, leftArmTo, new double[]{arm, 12, 4}, texture, 40, 16, deformation, true);
        } else if (slot == 2) {
            addInflatedBox(quads, bodyFrom, bodyTo, new double[]{8, 12, 4}, texture, 16, 16, deformation, false);
            addInflatedBox(quads, rightLegFrom, rightLegTo, new double[]{4, 12, 4}, texture, 0, 16, deformation, false);
            addInflatedBox(quads, leftLegFrom, leftLegTo, new double[]{4, 12, 4}, texture, 0, 16, deformation, true);
        } else {
            addInflatedBox(quads, rightLegFrom, rightLegTo, new double[]{4, 12, 4}, texture, 0, 16, deformation, false);
            addInflatedBox(quads, leftLegFrom, leftLegTo, new double[]{4, 12, 4}, texture, 0, 16, deformation, true);
        }
    }

    private void addInflatedBox(List<Quad> quads, double[] from, double[] to, double[] uvSize, BufferedImage texture, int u, int v, double deformation, boolean mirror) {
        double[] inflated = inflate(from, to, deformation);
        addBoxQuads(quads, new double[]{inflated[0], inflated[1], inflated[2]}, new double[]{inflated[3], inflated[4], inflated[5]}, uvSize, texture, u, v, mirror);
    }

    private void addHeldItems(List<Quad> quads, Object equipmentValue, boolean slim) throws IOException {
        if (!(equipmentValue instanceof Map<?, ?> equipment)) {
            return;
        }
        if (equipment.get("mainHand") instanceof Map<?, ?> mainHand) {
            addHeldItemQuads(quads, castMap(mainHand), false, slim);
        }
        if (equipment.get("offhand") instanceof Map<?, ?> offhand) {
            addHeldItemQuads(quads, castMap(offhand), true, slim);
        }
    }

    /**
     * Adds one held item to the same quad list as the player model, so it shares the depth sorting,
     * occlusion and rasteriser of the scene.
     *
     * <p>The chain is the vanilla one from 1.21.11 {@code ItemInHandLayer.renderArmWithItem}:
     * {@code translateToHand(arm)} -> {@code rotateX(-90)} -> {@code rotateY(180)} ->
     * {@code translate(±1/16, 0.125, -0.625)} and then the item model display transform. One world
     * unit of this renderer is one sixteenth of a block, which is exactly the unit vanilla uses for
     * item model coordinates, so every term below stays in skin pixels. The item geometry itself is
     * built in the item model space, which both vanilla and this renderer define with y pointing up
     * and the sprite facing +z, so only the surrounding entity model space has to be converted.</p>
     */
    private void addHeldItemQuads(List<Quad> quads, Map<String, Object> item, boolean offhand, boolean slim) throws IOException {
        String id = normalizeID(Json.string(item, "id", ""));
        if (id.isBlank()) {
            return;
        }
        ItemSelection selection = resolveItemSelection(id, item);
        List<Quad> local = new ArrayList<>();
        boolean frontLit;
        DisplayTransform display;
        try {
            if (selection.special != null) {
                // A special model is the same geometry in every display context; only the display
                // transform of the model the item points at changes, which the chain below applies.
                SpecialItem special = selection.special;
                addSpecialGeometry(local, special, item);
                frontLit = "front".equalsIgnoreCase(special.base.isBlank()
                        ? "" : loadModel(special.base).guiLight);
                display = specialDisplay(special, offhand ? "thirdperson_lefthand" : "thirdperson_righthand");
            } else {
                Model model = loadModelWithOverrides(selection.model, item);
                display = model.displayTransform(offhand ? "thirdperson_lefthand" : "thirdperson_righthand");
                if (model.isGenerated()) {
                    addGeneratedGeometry(local, model);
                } else if (!model.elements.isEmpty()) {
                    addElementGeometry(local, model);
                } else {
                    throw new IOException("item model has no renderable elements or generated layer: " + id);
                }
                frontLit = "front".equalsIgnoreCase(model.guiLight);
            }
        } catch (IOException | RuntimeException error) {
            System.err.println("held item " + id + " fell back to the missing model: " + error);
            local.clear();
            addElementGeometry(local, missingModel());
            frontLit = true;
            display = null;
        }
        Mat4 transform = heldItemTransform(offhand, slim, display);
        for (Quad quad : local) {
            quad.fullBright = frontLit;
            for (int index = 0; index < 4; index++) {
                double[] point = transform.point(quad.corners[index].x(), quad.corners[index].y(), quad.corners[index].z());
                quad.corners[index] = new Vec3(point[0], point[1], point[2]);
            }
            double[] normal = transform.normal(quad.normal.x(), quad.normal.y(), quad.normal.z());
            quad.normal = new Vec3(normal[0], normal[1], normal[2]);
            quads.add(quad);
        }
    }

    /**
     * Vanilla ItemInHandLayer / HumanoidModel.translateToHand built as a single matrix, prefixed by
     * the vanilla model space conversion. The five inner factors are the vanilla chain verbatim —
     * translateToHand(arm), {@code rotateX(-90)}, {@code rotateY(180)} and the
     * {@code translate(±1/16, 0.125, -0.625)} hand offset, all still in the vanilla entity model
     * space — so they can be compared against the decompiled {@code ItemInHandLayer} one to one.
     * {@link #sceneSpace()} then maps that pose into the scene space of this renderer, where the
     * feet stand at zero and the model faces the viewer, which puts the item at hand height in
     * front of the torso instead of behind its back.
     */
    private Mat4 heldItemTransform(boolean offhand, boolean slim, DisplayTransform display) {
        double pivotX = offhand ? PLAYER_LEFT_ARM_PIVOT_X : PLAYER_RIGHT_ARM_PIVOT_X;
        if (slim) {
            // PlayerModel.translateToHand moves the slim arm pivot 0.5 px towards the body.
            pivotX += offhand ? -0.5 : 0.5;
        }
        return sceneSpace()
                .multiply(Mat4.translation(pivotX, PLAYER_ARM_PIVOT_Y, 0))
                .multiply(Mat4.rotationX(-90))
                .multiply(Mat4.rotationY(180))
                .multiply(Mat4.translation(offhand ? -1 : 1, 2, -10))
                .multiply(displayMatrix(display, offhand));
    }

    /**
     * The vanilla entity model space expressed in the scene space used by every other constant of
     * this renderer. Vanilla poses living entities in a space that grows y downwards with its
     * origin at the neck — 24 skin pixels above the feet — and whose models look along -z, while
     * the scene space grows y upwards with the feet at the origin and the model facing the viewer
     * along +z. The two differ by a half turn around x, so the vanilla chain stays valid as is
     * once this factor is multiplied in front of it.
     *
     * <p>Both halves are visible in this file: the vanilla arm box spans y 0..12 and the legs
     * 12..24, {@link #addPlayerModel} draws the arm at 12..24 and the legs at 0..12, and the cloak
     * of vanilla sits at +z while {@link #addCape} hangs it behind the body at -z.</p>
     */
    private static Mat4 sceneSpace() {
        return Mat4.translation(0, PLAYER_MODEL_ORIGIN_Y, 0).multiply(Mat4.rotationX(180));
    }

    /**
     * Vanilla {@code ItemTransform#apply}: a translation in sixteenths of a block and a rotation in
     * degrees around X, Y and Z applied in that order, followed by the half block shift that
     * centres a 0..16 item model on the pose. The left hand mirrors the translation along X and the
     * Y/Z rotations.
     */
    private Mat4 displayMatrix(DisplayTransform display, boolean leftHand) {
        if (display == null) {
            return Mat4.translation(-8, -8, -8);
        }
        double translateX = leftHand ? -display.translation[0] : display.translation[0];
        double rotateY = leftHand ? -display.rotation[1] : display.rotation[1];
        double rotateZ = leftHand ? -display.rotation[2] : display.rotation[2];
        return Mat4.translation(translateX, display.translation[1], display.translation[2])
                .multiply(Mat4.rotationXyz(display.rotation[0], rotateY, rotateZ))
                .multiply(Mat4.scaling(display.scale[0], display.scale[1], display.scale[2]))
                .multiply(Mat4.translation(-8, -8, -8));
    }

    /** Model face geometry of a {@code minecraft:model} item, reusing the element face table of the item pipeline. */
    private void addElementGeometry(List<Quad> quads, Model model) throws IOException {
        for (Element element : model.elements) {
            for (Map.Entry<String, FaceDefinition> entry : element.faces.entrySet()) {
                List<Vec3> vertices = faceVertices(element, entry.getKey());
                if (vertices.size() < 4) {
                    continue;
                }
                FaceDefinition definition = entry.getValue();
                BufferedImage texture = resources.texture(model.texture(definition.texture, definition.texture));
                int[] uv = definition.uv == null ? defaultUV(element, entry.getKey()) : definition.uv;
                Quad quad = new Quad(faceNormal(entry.getKey()), texture);
                // faceVertices returns the vanilla cube order, the quad pipeline wants the corners in
                // texture rectangle order: (u0,v0) (u1,v0) (u0,v1) (u1,v1).
                int[] order = switch (entry.getKey()) {
                    case "up", "down" -> new int[]{0, 3, 1, 2};
                    default -> new int[]{0, 1, 3, 2};
                };
                for (int index = 0; index < 4; index++) {
                    quad.corners[index] = vertices.get(order[index]);
                }
                quad.uv[0] = uv[0];
                quad.uv[1] = uv[1];
                quad.uv[2] = uv[2];
                quad.uv[3] = uv[3];
                quads.add(quad);
            }
        }
    }

    private static Vec3 faceNormal(String direction) {
        return switch (direction) {
            case "north" -> new Vec3(0, 0, -1);
            case "south" -> new Vec3(0, 0, 1);
            case "west" -> new Vec3(-1, 0, 0);
            case "east" -> new Vec3(1, 0, 0);
            case "up" -> new Vec3(0, 1, 0);
            default -> new Vec3(0, -1, 0);
        };
    }

    /**
     * Vanilla ItemModelGenerator geometry: every layer is a full 16x16 plate that is one unit thick
     * and sits on {@code z} 7.5 to 8.5, plus one 1 px wide strip for every pixel edge that faces a
     * transparent pixel.
     */
    private void addGeneratedGeometry(List<Quad> quads, Model model) throws IOException {
        for (String layer : model.generatedLayers()) {
            BufferedImage texture = resources.texture(model.texture(layer, layer));
            addGeneratedPlate(quads, texture);
            addGeneratedOutline(quads, texture);
        }
    }

    /** The front and back plate of a generated layer. Both faces sample the full texture. */
    private void addGeneratedPlate(List<Quad> quads, BufferedImage texture) {
        quads.add(plate(texture, 8.5, new Vec3(0, 0, 1)));
        // The back face uses the mirrored vanillla uv rectangle, which is the same as sampling the
        // texture with u growing along +x while looking at the plate from behind.
        quads.add(plate(texture, 7.5, new Vec3(0, 0, -1)));
    }

    private Quad plate(BufferedImage texture, double z, Vec3 normal) {
        Quad quad = new Quad(normal, texture);
        quad.corners[0] = new Vec3(0, 16, z);
        quad.corners[1] = new Vec3(16, 16, z);
        quad.corners[2] = new Vec3(0, 0, z);
        quad.corners[3] = new Vec3(16, 0, z);
        quad.uv[0] = 0;
        quad.uv[1] = 0;
        quad.uv[2] = 16;
        quad.uv[3] = 16;
        return quad;
    }

    private void addGeneratedOutline(List<Quad> quads, BufferedImage texture) {
        int width = texture.getWidth();
        int height = texture.getHeight();
        double scaleX = 16.0 / width;
        double scaleY = 16.0 / height;
        for (int y = 0; y < height; y++) {
            for (int x = 0; x < width; x++) {
                if (!opaque(texture, x, y)) {
                    continue;
                }
                double x0 = x * scaleX;
                double x1 = (x + 1) * scaleX;
                double y0 = 16 - (y + 1) * scaleY;
                double y1 = 16 - y * scaleY;
                double u0 = (x + 0.1) * scaleX;
                double u1 = (x + 0.9) * scaleX;
                double v0 = (y + 0.1) * scaleY;
                double v1 = (y + 0.9) * scaleY;
                if (!opaque(texture, x, y - 1)) {
                    quads.add(strip(texture, new Vec3(0, 1, 0), new double[]{u0, v0, u1, v1},
                            new Vec3(x0, y1, 8.5), new Vec3(x1, y1, 8.5), new Vec3(x0, y1, 7.5), new Vec3(x1, y1, 7.5)));
                }
                if (!opaque(texture, x, y + 1)) {
                    quads.add(strip(texture, new Vec3(0, -1, 0), new double[]{u0, v0, u1, v1},
                            new Vec3(x0, y0, 7.5), new Vec3(x0, y0, 8.5), new Vec3(x1, y0, 7.5), new Vec3(x1, y0, 8.5)));
                }
                if (!opaque(texture, x - 1, y)) {
                    quads.add(strip(texture, new Vec3(-1, 0, 0), new double[]{u0, v0, u1, v1},
                            new Vec3(x0, y1, 7.5), new Vec3(x0, y1, 8.5), new Vec3(x0, y0, 7.5), new Vec3(x0, y0, 8.5)));
                }
                if (!opaque(texture, x + 1, y)) {
                    quads.add(strip(texture, new Vec3(1, 0, 0), new double[]{u0, v0, u1, v1},
                            new Vec3(x1, y1, 8.5), new Vec3(x1, y1, 7.5), new Vec3(x1, y0, 8.5), new Vec3(x1, y0, 7.5)));
                }
            }
        }
    }

    private Quad strip(BufferedImage texture, Vec3 normal, double[] uv, Vec3 a, Vec3 b, Vec3 c, Vec3 d) {
        Quad quad = new Quad(normal, texture);
        quad.corners[0] = a;
        quad.corners[1] = b;
        quad.corners[2] = c;
        quad.corners[3] = d;
        System.arraycopy(uv, 0, quad.uv, 0, 4);
        return quad;
    }

    /** Out of bounds neighbours count as transparent, exactly like the vanilla outline pass. */
    private static boolean opaque(BufferedImage texture, int x, int y) {
        if (x < 0 || y < 0 || x >= texture.getWidth() || y >= texture.getHeight()) {
            return false;
        }
        return (texture.getRGB(x, y) >>> 24) != 0;
    }

    private BufferedImage renderPlayer(Map<String, Object> request, int width, int height) throws IOException {
        if (width < 32 || height < 48 || width > 4096 || height > 4096) {
            throw new IOException("player render dimensions are outside limits");
        }
        // The model is fitted to the layout canvas and then projected at the render scale, so a
        // magnified skin is sampled at the resolution it is delivered at.
        Scene scene = new Scene(PLAYER_VIEW_YAW, PLAYER_VIEW_PITCH);
        List<Quad> quads = buildPlayerQuads(request, scene, width, height, true);
        BufferedImage canvas = canvas(width, height);
        paintScene(canvas, quads, scene, pixelScale);
        return canvas;
    }

    /**
     * Builds the player model, the equipment layers and the held items as one quad list.
     *
     * <p>When {@code fit} is set — the {@code player} and {@code skin} operations — the scene is
     * scaled so everything that is drawn, held items included, fits the canvas. The interactive
     * inventory instead fixes the viewport to the vanilla InventoryScreen values and passes
     * {@code fit} as false, so there the held item can never influence the model scale either
     * way.</p>
     */
    private List<Quad> buildPlayerQuads(Map<String, Object> request, Scene scene, int width, int height, boolean fit) throws IOException {
        Map<String, Object> skin = Json.object(request.get("skin"), "skin");
        BufferedImage texture = skinTexture(skin);
        boolean slim = Json.bool(skin, "slim", false) || "slim".equalsIgnoreCase(Json.string(skin, "model", ""));
        boolean outer = Json.bool(skin, "outerLayer", false);
        BufferedImage cape = capeTexture(skin);
        Object equipmentValue = request.get("equipment");
        List<Quad> quads = new ArrayList<>();
        if (cape != null) {
            addCape(quads, cape);
        }
        addPlayerModel(quads, texture, slim, outer);
        addEquipmentLayers(quads, equipmentValue, slim, cape);
        addHeldItems(quads, equipmentValue, slim);
        if (fit) {
            scene.fit(quads, width, height);
        }
        return quads;
    }

    private BufferedImage skinTexture(Map<String, Object> skin) throws IOException {
        String value = Json.string(skin, "texture", "");
        if (value.isBlank()) {
            throw new IOException("skin.texture must contain an inline 64x64 PNG or a resource path");
        }
        BufferedImage image = decodeTextureValue(value);
        if (image.getWidth() != 64 || (image.getHeight() != 64 && image.getHeight() != 32)) {
            throw new IOException("skin texture must be 64x64 or legacy 64x32 PNG");
        }
        if (image.getHeight() == 32) {
            // Legacy skins only store the right limbs; vanilla mirrors them into the modern
            // left arm and left leg rectangles while converting the texture.
            BufferedImage expanded = new BufferedImage(64, 64, BufferedImage.TYPE_INT_ARGB);
            Graphics2D graphics = expanded.createGraphics();
            graphics.drawImage(image, 0, 0, null);
            graphics.dispose();
            mirrorRegion(image, expanded, 0, 16);
            mirrorRegion(image, expanded, 40, 16);
            image = expanded;
        }
        return image;
    }

    private void mirrorRegion(BufferedImage source, BufferedImage destination, int x, int y) {
        int targetX = x == 40 ? 32 : 16;
        for (int row = 0; row < 16; row++) {
            for (int column = 0; column < 16; column++) {
                destination.setRGB(targetX + 15 - column, 48 + row, source.getRGB(x + column, y + row));
            }
        }
    }

    private BufferedImage capeTexture(Map<String, Object> skin) throws IOException {
        String value = Json.string(skin, "cape", "");
        if (value.isBlank()) {
            return null;
        }
        BufferedImage image = decodeTextureValue(value);
        if ((image.getWidth() != 64 || image.getHeight() != 32) && (image.getWidth() != 22 || image.getHeight() != 17)) {
            throw new IOException("cape texture must be 64x32 or 22x17 PNG");
        }
        return image;
    }

    private BufferedImage decodeTextureValue(String value) throws IOException {
        BufferedImage image;
        if (value.startsWith("data:image/png;base64,")) {
            try {
                image = decodeImage(Base64.getDecoder().decode(value.substring("data:image/png;base64,".length())));
            } catch (IllegalArgumentException error) {
                throw new IOException("invalid base64 PNG texture", error);
            }
        } else {
            try {
                image = decodeImage(Base64.getDecoder().decode(value));
            } catch (IllegalArgumentException ignored) {
                image = resources.texture(value);
            }
        }
        return image;
    }

    private BufferedImage decodeImage(byte[] data) throws IOException {
        BufferedImage image = ImageIO.read(new ByteArrayInputStream(data));
        if (image == null) {
            throw new IOException("skin texture is not a PNG image");
        }
        return image;
    }

    private void drawCrop(Graphics2D graphics, BufferedImage source, int x, int y, int width, int height, int sx, int sy, int sw, int sh) {
        if (sx < 0 || sy < 0 || sx + sw > source.getWidth() || sy + sh > source.getHeight()) {
            return;
        }
        graphics.drawImage(source, x, y, x + width, y + height, sx, sy, sx + sw, sy + sh, null);
    }

    private void drawScaled(BufferedImage destination, BufferedImage source, int x, int y, int width, int height) {
        Graphics2D graphics = graphics(destination);
        graphics.drawImage(source, x, y, width, height, null);
        graphics.dispose();
    }

    private boolean hasGlint(Map<String, Object> item) {
        Object enchantments = item.get("enchantments");
        if (enchantments instanceof List<?> list && !list.isEmpty()) {
            return true;
        }
        Object components = item.get("components");
        return components instanceof Map<?, ?> map && (map.containsKey("minecraft:enchantments") || map.containsKey("enchantments"));
    }

    private void drawGlint(BufferedImage image) {
        Graphics2D graphics = image.createGraphics();
        graphics.setComposite(AlphaComposite.getInstance(AlphaComposite.SRC_OVER, 0.30f));
        graphics.setColor(new Color(195, 85, 255));
        int step = Math.max(5, image.getWidth() / 9);
        for (int x = -image.getHeight(); x < image.getWidth(); x += step) {
            graphics.rotate(Math.toRadians(25), image.getWidth() / 2.0, image.getHeight() / 2.0);
            graphics.fillRect(x, 0, Math.max(2, step / 3), image.getHeight());
            graphics.rotate(Math.toRadians(-25), image.getWidth() / 2.0, image.getHeight() / 2.0);
        }
        graphics.dispose();
    }

    private void drawDurability(BufferedImage image, int damage, int maxDamage) {
        int width = image.getWidth();
        int y = image.getHeight() - Math.max(2, image.getHeight() / 18);
        int remaining = Math.max(0, Math.min(width, (int) Math.round(width * (1.0 - damage / (double) maxDamage))));
        Graphics2D graphics = image.createGraphics();
        graphics.setColor(new Color(0, 0, 0, 210));
        graphics.fillRect(0, y, width, Math.max(2, image.getHeight() / 22));
        graphics.setColor(new Color(42, 210, 62, 255));
        graphics.fillRect(0, y, remaining, Math.max(2, image.getHeight() / 22));
        graphics.dispose();
    }

    private boolean isTransparent(BufferedImage image) {
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if ((image.getRGB(x, y) >>> 24) != 0) {
                    return false;
                }
            }
        }
        return true;
    }

    private int shade(String direction) {
        return switch (direction) {
            case "down" -> 70;
            case "north", "west" -> 18;
            default -> 0;
        };
    }

    private static Map<String, Object> castMap(Map<?, ?> map) {
        @SuppressWarnings("unchecked")
        Map<String, Object> value = (Map<String, Object>) map;
        return value;
    }

    private static String normalizeID(String id) {
        id = id == null ? "" : id.trim();
        if (id.startsWith("#")) {
            return id.substring(1);
        }
        if (id.indexOf(':') < 0 && !id.isBlank()) {
            return "minecraft:" + id;
        }
        return id;
    }

    /**
     * Vanilla {@code PartPose} of a {@code PartDefinition}: an offset in sixteenths of a block, a
     * rotation in radians around x, y and z and a uniform scale, applied in that order.
     */
    private record PartPose(double[] offset, double x, double y, double z, double scale) {
        static final PartPose ZERO = new PartPose(new double[]{0, 0, 0}, 0, 0, 0, 1);

        static PartPose offset(double x, double y, double z) {
            return new PartPose(new double[]{x, y, z}, 0, 0, 0, 1);
        }

        static PartPose rotation(double x, double y, double z) {
            return new PartPose(new double[]{0, 0, 0}, x, y, z, 1);
        }

        static PartPose offsetAndRotation(double x, double y, double z, double xRot, double yRot, double zRot) {
            return new PartPose(new double[]{x, y, z}, xRot, yRot, zRot, 1);
        }

        PartPose scaled(double value) {
            return new PartPose(offset, x, y, z, value);
        }

        /** Vanilla {@code ModelPart#translateAndRotate} followed by the scale of the pose. */
        Mat4 matrix() {
            return Mat4.translation(offset[0], offset[1], offset[2])
                    .multiply(Mat4.rotationZ(Math.toDegrees(z)))
                    .multiply(Mat4.rotationY(Math.toDegrees(y)))
                    .multiply(Mat4.rotationX(Math.toDegrees(x)))
                    .multiply(Mat4.scaling(scale, scale, scale));
        }
    }


    private record Vec3(double x, double y, double z) {
    }

    private record Point(double x, double y) {
    }

    private record Face(Path2D polygon, BufferedImage texture, double depth, int shade, boolean tint, int[] uv, int rotation) {
    }

    static final class ResourceStack {
        private final List<Path> roots;
        private final Map<String, BufferedImage> textureCache = new HashMap<>();
        /**
         * Textures that arrive with the request instead of in the resource tree, keyed by the relative
         * path the lookup would have used. They are the images of custom items, whose packs live
         * outside the tree the service downloaded.
         */
        private final Map<String, BufferedImage> inlineTextures = new HashMap<>();
        private final Set<String> missingWarnings = java.util.Collections.newSetFromMap(new HashMap<>());
        private BufferedImage missingTexture;
        private Map<String, Object> language;

        private ResourceStack(List<Path> roots) {
            this.roots = roots;
        }

        static ResourceStack from(Map<String, Object> request, String resourceRoot) {
            ArrayList<Path> roots = new ArrayList<>();
            Path root = Path.of(resourceRoot).toAbsolutePath().normalize();
            roots.add(root);
            roots.add(root.resolve("vanilla"));
            roots.add(root.resolve("addon"));
            Object packValue = request.get("packRoots");
            if (packValue instanceof List<?> packs) {
                for (Object pack : packs) {
                    if (pack instanceof String path && !path.isBlank()) {
                        roots.add(Path.of(path).toAbsolutePath().normalize());
                    }
                }
            }
            return new ResourceStack(roots);
        }

        Map<String, Object> json(String relative) throws IOException {
            byte[] data = bytes(relative);
            if (data == null) {
                return null;
            }
            Object value;
            try {
                value = Json.parse(new String(data, StandardCharsets.UTF_8));
            } catch (IllegalArgumentException error) {
                throw new IOException("invalid resource JSON " + relative + ": " + error.getMessage(), error);
            }
            return Json.object(value, relative);
        }

        /**
         * Reads a texture, falling back to the generated missing texture. A missing sprite degrades
         * that single layer of the picture instead of failing the whole render; the reason is written
         * to stderr once per texture so a broken pack stays diagnosable.
         */
        BufferedImage texture(String textureID) throws IOException {
            if (MISSING_TEXTURE.equals(textureID)) {
                return missingTexture();
            }
            String relative = texturePath(textureID);
            BufferedImage inline = inlineTextures.get(relative);
            if (inline != null) {
                return inline;
            }
            BufferedImage cached = textureCache.get(relative);
            if (cached != null) {
                return cached;
            }
            byte[] data = bytes(relative);
            if (data == null) {
                if (missingWarnings.add(relative)) {
                    System.err.println("missing texture " + textureID + ", drawing the missing texture instead");
                }
                return missingTexture();
            }
            BufferedImage image = ImageIO.read(new ByteArrayInputStream(data));
            if (image == null) {
                if (missingWarnings.add(relative)) {
                    System.err.println("invalid PNG texture " + textureID + ", drawing the missing texture instead");
                }
                return missingTexture();
            }
            textureCache.put(relative, image);
            return image;
        }

        /** Whether a texture file exists, for the call sites that must still report a bad reference. */
        boolean hasTexture(String textureID) throws IOException {
            if (MISSING_TEXTURE.equals(textureID)) {
                return true;
            }
            String relative = texturePath(textureID);
            return inlineTextures.containsKey(relative) || bytes(relative) != null;
        }

        /**
         * Registers a texture that comes with the request, as the data URI the custom content index
         * produced. A texture that does not decode is skipped with a note: one broken layer must not
         * take the whole picture down.
         */
        void putInlineTexture(String textureID, String value) {
            String prefix = "data:image/png;base64,";
            if (value == null || !value.startsWith(prefix)) {
                return;
            }
            try {
                byte[] data = Base64.getDecoder().decode(value.substring(prefix.length()));
                BufferedImage image = ImageIO.read(new ByteArrayInputStream(data));
                if (image == null) {
                    System.err.println("inline texture " + textureID + " is not a PNG, skipping it");
                    return;
                }
                inlineTextures.put(texturePath(textureID), image);
            } catch (IOException | IllegalArgumentException error) {
                System.err.println("inline texture " + textureID + " is invalid, skipping it");
            }
        }

        /**
         * The vanilla missing texture, generated instead of loaded so that it is available in every
         * resource context: a 16x16 checkerboard of two 8x8 quadrants, black on the top left and the
         * bottom right and {@code 0xFFF800F8} on the other two.
         */
        BufferedImage missingTexture() {
            if (missingTexture == null) {
                BufferedImage image = new BufferedImage(16, 16, BufferedImage.TYPE_INT_ARGB);
                for (int y = 0; y < 16; y++) {
                    for (int x = 0; x < 16; x++) {
                        image.setRGB(x, y, (x < 8) ^ (y < 8) ? 0xFFF800F8 : 0xFF000000);
                    }
                }
                missingTexture = image;
            }
            return missingTexture;
        }

        /** Sprite metadata of a texture, used for the tooltip nine slice borders. Null when absent. */
        Map<String, Object> metadata(String textureID) throws IOException {
            byte[] data = bytes(texturePath(textureID) + ".mcmeta");
            if (data == null) {
                return null;
            }
            return Json.object(Json.parse(new String(data, StandardCharsets.UTF_8)), "mcmeta");
        }

        /** Vanilla language lookup; the built in pack ships en_us.json. */
        String language(String key) throws IOException {
            if (language == null) {
                byte[] data = bytes("assets/minecraft/lang/en_us.json");
                language = data == null
                        ? Json.object(Map.of(), "lang")
                        : Json.object(Json.parse(new String(data, StandardCharsets.UTF_8)), "lang");
            }
            Object value = language.get(key);
            return value instanceof String string ? string : "";
        }

        private static String texturePath(String textureID) throws IOException {
            String normalized = textureID == null ? "" : textureID.trim();
            if (normalized.startsWith("#")) {
                normalized = normalized.substring(1);
            }
            if (normalized.isBlank()) {
                throw new IOException("model texture reference is empty");
            }
            int separator = normalized.indexOf(':');
            String namespace = separator < 0 ? "minecraft" : normalized.substring(0, separator);
            String path = separator < 0 ? normalized : normalized.substring(separator + 1);
            path = path.replaceFirst("^textures/", "");
            if (!path.endsWith(".png")) {
                path += ".png";
            }
            return "assets/" + namespace + "/textures/" + path;
        }

        /**
         * Reads one image of the stack without the missing texture fallback, for the files a font
         * provider slices itself. A sheet that is absent answers null so the font can skip it.
         */
        BufferedImage image(String relative) throws IOException {
            byte[] data = bytes(relative);
            if (data == null) {
                return null;
            }
            return ImageIO.read(new ByteArrayInputStream(data));
        }

        byte[] bytes(String relative) throws IOException {
            if (relative.startsWith("/") || relative.contains("..") || relative.indexOf('\0') >= 0) {
                throw new IOException("unsafe resource path " + relative);
            }
            for (int i = roots.size() - 1; i >= 0; i--) {
                Path root = roots.get(i);
                Path candidate = root.resolve(relative).normalize();
                if (!candidate.startsWith(root)) {
                    throw new IOException("resource path escapes pack root");
                }
                if (Files.isRegularFile(candidate)) {
                    return Files.readAllBytes(candidate);
                }
            }
            return null;
        }
    }

    private static final class Model {
        final String id;
        String parent = "";
        String guiLight = "";
        final Map<String, String> textures = new LinkedHashMap<>();
        final List<Element> elements = new ArrayList<>();
        final List<Override> overrides = new ArrayList<>();
        final Map<String, DisplayTransform> display = new LinkedHashMap<>();
        boolean hasElements;

        Model(String id) {
            this.id = id;
        }

        static Model from(String id, Map<String, Object> raw) {
            Model model = new Model(id);
            model.parent = Json.string(raw, "parent", "");
            model.guiLight = Json.string(raw, "gui_light", "");
            Object textures = raw.get("textures");
            if (textures instanceof Map<?, ?> map) {
                for (Map.Entry<?, ?> entry : map.entrySet()) {
                    if (entry.getKey() instanceof String key && entry.getValue() instanceof String value) {
                        model.textures.put(key, value);
                    }
                }
            }
            Object elements = raw.get("elements");
            if (elements instanceof List<?> list) {
                model.hasElements = true;
                for (Object value : list) {
                    if (value instanceof Map<?, ?> map) {
                        model.elements.add(Element.from(castMap(map)));
                    }
                }
            }
            Object display = raw.get("display");
            if (display instanceof Map<?, ?> map) {
                for (Map.Entry<?, ?> entry : map.entrySet()) {
                    if (entry.getKey() instanceof String key && entry.getValue() instanceof Map<?, ?> value) {
                        model.display.put(key, DisplayTransform.from(castMap(value)));
                    }
                }
                // ModelDisplay deserialisation mirrors the right hand onto the left hand when the
                // model does not define it, so the fallback has to happen per model file.
                for (String[] pair : new String[][]{
                        {"firstperson_lefthand", "firstperson_righthand"},
                        {"thirdperson_lefthand", "thirdperson_righthand"}}) {
                    if (!model.display.containsKey(pair[0]) && model.display.containsKey(pair[1])) {
                        model.display.put(pair[0], model.display.get(pair[1]));
                    }
                }
            }
            Object overrides = raw.get("overrides");
            if (overrides instanceof List<?> list) {
                for (Object value : list) {
                    if (value instanceof Map<?, ?> map) {
                        Map<String, Object> object = castMap(map);
                        Object predicate = object.get("predicate");
                        if (predicate instanceof Map<?, ?> predicateMap) {
                            model.overrides.add(new Override(Json.string(object, "model", ""), castMap(predicateMap)));
                        }
                    }
                }
            }
            return model;
        }

        Model copyFor(String newID) {
            Model copy = new Model(newID);
            copy.parent = parent;
            copy.guiLight = guiLight;
            copy.textures.putAll(textures);
            copy.elements.addAll(elements);
            copy.overrides.addAll(overrides);
            copy.display.putAll(display);
            copy.hasElements = hasElements;
            return copy;
        }

        void merge(Model other) {
            parent = other.parent;
            if (!other.guiLight.isBlank()) {
                guiLight = other.guiLight;
            }
            textures.putAll(other.textures);
            if (other.hasElements) {
                elements.clear();
                elements.addAll(other.elements);
                hasElements = true;
            }
            if (!other.overrides.isEmpty()) {
                overrides.clear();
                overrides.addAll(other.overrides);
            }
            display.putAll(other.display);
        }

        DisplayTransform displayTransform(String name) {
            return display.get(name);
        }

        List<String> generatedLayers() {
            ArrayList<String> layers = new ArrayList<>();
            for (int index = 0; index < 32; index++) {
                String layer = "layer" + index;
                if (!textures.containsKey(layer)) {
                    break;
                }
                layers.add(layer);
            }
            return layers;
        }

        String texture(String name, String fallback) {
            String value = textures.getOrDefault(name, fallback);
            for (int depth = 0; depth < 12 && value.startsWith("#"); depth++) {
                String resolved = textures.get(value.substring(1));
                if (resolved == null || resolved.equals(value)) {
                    break;
                }
                value = resolved;
            }
            return value;
        }

        boolean isGenerated() {
            if (!elements.isEmpty()) {
                return false;
            }
            return parent.endsWith("item/generated")
                    || parent.endsWith("item/handheld")
                    || (elements.isEmpty() && textures.containsKey("layer0"));
        }
    }

    private static final class DisplayTransform {
        final double[] rotation;
        final double[] translation;
        final double[] scale;

        DisplayTransform(double[] rotation, double[] translation, double[] scale) {
            this.rotation = rotation;
            this.translation = translation;
            this.scale = scale;
        }

        static DisplayTransform from(Map<String, Object> raw) {
            return new DisplayTransform(
                    vector(raw.get("rotation"), new double[]{0, 0, 0}, 0),
                    vector(raw.get("translation"), new double[]{0, 0, 0}, 80),
                    vector(raw.get("scale"), new double[]{1, 1, 1}, 4));
        }

        /**
         * ItemTransform deserialisation clamps the translation to five blocks and the scale to four,
         * and the translation is expressed in sixteenths of a block, which is this renderer's unit.
         */
        private static double[] vector(Object value, double[] fallback, double limit) {
            if (value instanceof List<?> list && list.size() >= 3) {
                return new double[]{
                        limited(number(list.get(0)), fallback[0], limit),
                        limited(number(list.get(1)), fallback[1], limit),
                        limited(number(list.get(2)), fallback[2], limit)};
            }
            return fallback;
        }

        private static double limited(double value, double fallback, double limit) {
            if (!Double.isFinite(value)) {
                return fallback;
            }
            return limit <= 0 ? value : Math.max(-limit, Math.min(limit, value));
        }

        private static double number(Object value) {
            return value instanceof Number number ? number.doubleValue() : 0;
        }
    }

    private static final class Element {
        final double[] from;
        final double[] to;
        final Map<String, FaceDefinition> faces;

        Element(double[] from, double[] to, Map<String, FaceDefinition> faces) {
            this.from = from;
            this.to = to;
            this.faces = faces;
        }

        static Element from(Map<String, Object> raw) {
            return new Element(
                    vector(raw.get("from"), new double[]{0, 0, 0}),
                    vector(raw.get("to"), new double[]{16, 16, 16}),
                    faces(raw.get("faces")));
        }

        private static Map<String, FaceDefinition> faces(Object value) {
            LinkedHashMap<String, FaceDefinition> result = new LinkedHashMap<>();
            if (value instanceof Map<?, ?> map) {
                for (Map.Entry<?, ?> entry : map.entrySet()) {
                    if (entry.getKey() instanceof String key && entry.getValue() instanceof Map<?, ?> face) {
                        result.put(key, FaceDefinition.from(castMap(face)));
                    }
                }
            }
            return result;
        }

        private static double[] vector(Object value, double[] fallback) {
            if (value instanceof List<?> list && list.size() >= 3) {
                return new double[]{number(list.get(0)), number(list.get(1)), number(list.get(2))};
            }
            return fallback;
        }

        private static double number(Object value) {
            return value instanceof Number number ? number.doubleValue() : 0;
        }
    }

    private static final class FaceDefinition {
        final String texture;
        final int tintIndex;
        final int[] uv;
        final int rotation;

        FaceDefinition(String texture, int tintIndex, int[] uv, int rotation) {
            this.texture = texture;
            this.tintIndex = tintIndex;
            this.uv = uv;
            this.rotation = rotation;
        }

        static FaceDefinition from(Map<String, Object> raw) {
            return new FaceDefinition(
                    Json.string(raw, "texture", ""),
                    Json.integer(raw, "tintindex", -1),
                    vector(raw.get("uv")),
                    Json.integer(raw, "rotation", 0));
        }

        private static int[] vector(Object value) {
            if (!(value instanceof List<?> list) || list.size() < 4) {
                return null;
            }
            int[] result = new int[4];
            for (int index = 0; index < result.length; index++) {
                result[index] = list.get(index) instanceof Number number ? number.intValue() : 0;
            }
            return result;
        }
    }

    private static final class Override {
        final String model;
        final Map<String, Object> predicate;

        Override(String model, Map<String, Object> predicate) {
            this.model = model;
            this.predicate = predicate;
        }

        boolean matches(Map<String, Object> item) {
            for (Map.Entry<String, Object> entry : predicate.entrySet()) {
                double expected = entry.getValue() instanceof Number number ? number.doubleValue() : 0;
                double actual = switch (entry.getKey()) {
                    case "custom_model_data" -> Json.number(item, "customModelData", 0);
                    case "damage" -> Json.number(item, "damage", 0);
                    default -> 0;
                };
                if (actual < expected) {
                    return false;
                }
            }
            return true;
        }
    }
}
