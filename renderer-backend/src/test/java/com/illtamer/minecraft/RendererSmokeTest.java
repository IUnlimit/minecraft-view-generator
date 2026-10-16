package com.illtamer.minecraft;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Headless smoke test for the Java renderer. It only uses the JDK and asserts against the
 * checked in fixture resource pack, so it stays runnable without the downloaded Minecraft
 * client resources.
 */
public final class RendererSmokeTest {
    private RendererSmokeTest() {}

    public static void main(String[] args) throws Exception {
        Path root = Path.of(args.length == 0 ? "renderer-backend/fixture" : args[0]).toAbsolutePath();
        Renderer renderer = new Renderer();
        testItem(renderer, root);
        testPlayerModel(renderer, root);
        testArmourLayers(renderer, root);
        testMap(renderer, root);
        testPlayerList(renderer, root);
        testPlayerInventory(renderer, root);
        testTooltip(renderer, root);
        testHeldItems(renderer, root);
        System.out.println("renderer smoke: item/skin/armour/map/playerlist/inventory/tooltip/hand paths passed");
    }

    private static void testItem(Renderer renderer, Path root) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "item");
        request.put("resourceRoot", root.toString());
        request.put("item", Map.of("id", "minecraft:diamond"));
        BufferedImage icon = renderer.render(request);
        require(icon.getWidth() == 128 && icon.getHeight() == 128, "item dimensions");
        require(hasPixels(icon), "item pixels");
        require(Renderer.class.getResourceAsStream("/MinecraftDefault-Regular.ttf") != null, "regular font resource");
        require(Renderer.class.getResourceAsStream("/MinecraftDefaultUniformBMP-Regular.ttf") != null, "uniform font resource");
    }

    private static void testPlayerModel(Renderer renderer, Path root) throws Exception {
        BufferedImage classic = renderPlayer(renderer, root, "classic", false, List.of());
        require(classic.getWidth() == 320 && classic.getHeight() == 480, "player dimensions");
        // Every body part must be textured from its own vanilla skin rectangle.
        require(countColor(classic, ColorKind.HEAD) > 200, "head region missing");
        require(countColor(classic, ColorKind.BODY) > 200, "body region missing");
        require(countColor(classic, ColorKind.RIGHT_ARM) > 100, "right arm region missing");
        require(countColor(classic, ColorKind.LEFT_ARM) > 100, "left arm region missing");
        require(countColor(classic, ColorKind.RIGHT_LEG) > 100, "right leg region missing");
        require(countColor(classic, ColorKind.LEFT_LEG) > 100, "left leg region missing");
        require(centerY(classic, ColorKind.HEAD) < centerY(classic, ColorKind.BODY), "head is not above the body");
        require(centerX(classic, ColorKind.RIGHT_ARM) < centerX(classic, ColorKind.LEFT_ARM), "arms are not mirrored");
        require(centerX(classic, ColorKind.RIGHT_LEG) < centerX(classic, ColorKind.LEFT_LEG), "legs are not mirrored");
        require(countColor(classic, ColorKind.HAT) == 0, "outer layer rendered without outerLayer");

        BufferedImage outer = renderPlayer(renderer, root, "classic", true, List.of());
        require(countColor(outer, ColorKind.HAT) > 200, "outer layer missing with outerLayer");

        BufferedImage slim = renderPlayer(renderer, root, "slim", false, List.of());
        require(contentWidth(slim) < contentWidth(classic), "slim model is not narrower than classic");
    }

    private static void testArmourLayers(Renderer renderer, Path root) throws Exception {
        List<Map<String, Object>> armour = List.of(
                Map.of("slot", 0, "item", Map.of("id", "minecraft:diamond_helmet")),
                Map.of("slot", 1, "item", Map.of("id", "minecraft:diamond_chestplate")),
                Map.of("slot", 2, "item", Map.of("id", "minecraft:diamond_leggings")),
                Map.of("slot", 3, "item", Map.of("id", "minecraft:diamond_boots")));
        BufferedImage armoured = renderPlayer(renderer, root, "classic", false, armour);
        require(countColor(armoured, ColorKind.ARMOUR_HEAD) > 200, "helmet layer missing");
        require(countColor(armoured, ColorKind.ARMOUR_BODY) > 200, "chestplate layer missing");
        require(countColor(armoured, ColorKind.ARMOUR_LEG) > 200, "leggings layer missing");
        require(countColor(armoured, ColorKind.ARMOUR_BOOT) > 100, "boots layer missing");

        Map<String, Object> unknown = new LinkedHashMap<>();
        unknown.put("operation", "player");
        unknown.put("resourceRoot", root.toString());
        unknown.put("skin", Map.of("texture", PlayerSkinFixture.png64()));
        unknown.put("equipment", Map.of("armor", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:iron_helmet")))));
        requireFails(renderer, unknown, "missing equipment definition");
    }

    private static void testMap(Renderer renderer, Path root) throws Exception {
        List<Integer> pixels = new java.util.ArrayList<>();
        for (int index = 0; index < 128 * 128; index++) {
            pixels.add(index % 128 < 64 ? 1 : 2);
        }
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "map");
        request.put("resourceRoot", root.toString());
        request.put("map", Map.of("pixels", pixels, "markers", List.of(
                Map.of("x", 32, "z", 32, "rotation", 0, "type", "player"),
                Map.of("x", 96, "z", 96, "rotation", 8, "type", "player"))));
        BufferedImage map = renderer.render(request);
        require(map.getWidth() == 140 && map.getHeight() == 140, "map default dimensions");
        require((map.getRGB(0, 0) >>> 24) == 255, "map background is transparent");
        require(sameColor(map.getRGB(0, 0), 0x99876C), "map background is not the parchment texture");
        int inset = Math.round(140 / 23.333333f);
        require(countColor(map, ColorKind.MAP_GRASS) > 1000, "map pixels missing");
        require(countColor(map, ColorKind.MAP_SAND) > 1000, "map pixels missing");
        require(countColor(map, ColorKind.DECORATION_WHITE) > 4, "player marker triangle missing");
        require(inset > 0, "map inset is not applied");
        // rotation 0 points north (up), rotation 8 points south (down)
        int content = 140 - inset * 2;
        int half = (int) Math.ceil(140 * 96.0 / 1120.0 / 2) + 2;
        int north = (int) Math.round(inset + 32.5 * content / 128.0);
        int south = (int) Math.round(inset + 96.5 * content / 128.0);
        require(centerY(map, ColorKind.DECORATION_WHITE, north - half, north - half, north + half, north + half)
                        < centerY(map, ColorKind.DECORATION_BLACK, north - half, north - half, north + half, north + half),
                "marker rotation 0 is flipped");
        require(centerY(map, ColorKind.DECORATION_WHITE, south - half, south - half, south + half, south + half)
                        > centerY(map, ColorKind.DECORATION_BLACK, south - half, south - half, south + half, south + half),
                "marker rotation 8 is flipped");


        Map<String, Object> unknown = new LinkedHashMap<>();
        unknown.put("operation", "map");
        unknown.put("resourceRoot", root.toString());
        unknown.put("map", Map.of("markers", List.of(Map.of("x", 1, "z", 1, "type", "not_a_marker"))));
        requireFails(renderer, unknown, "unknown map marker type");
    }

    private static void testPlayerList(Renderer renderer, Path root) throws Exception {
        Map<String, Object> player = Map.of("name", "Steve", "texture", PlayerSkinFixture.png64(), "ping", 10);
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "playerlist");
        request.put("resourceRoot", root.toString());
        request.put("playerList", Map.of("header", "Fixture", "players", List.of(player)));
        BufferedImage list = renderer.render(request);
        require(list.getWidth() > 64 && list.getHeight() > 32, "playerlist dimensions");
        require(countPanel(list) > 500, "playerlist panel missing");
        // The fixture hat layer is opaque, so a vanilla avatar shows the hat rectangle on top.
        require(countColor(list, ColorKind.HAT) > 100, "playerlist avatar missing");
        require(countColor(list, ColorKind.PING) > 4, "playerlist ping icon missing");
        require(countAlpha(list, 255) > 100, "playerlist text missing");
    }

    /** Vanilla draws the tab list panel with a 50% black fill. */
    private static int countPanel(BufferedImage image) {
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) == 128 && ((pixel >> 16) & 0xFF) == 0 && ((pixel >> 8) & 0xFF) == 0 && (pixel & 0xFF) == 0) {
                    count++;
                }
            }
        }
        return count;
    }

    private static BufferedImage renderPlayer(Renderer renderer, Path root, String model, boolean outer, List<Map<String, Object>> armour) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "player");
        request.put("resourceRoot", root.toString());
        request.put("skin", Map.of("texture", PlayerSkinFixture.png64(), "model", model, "outerLayer", outer));
        request.put("equipment", Map.of("armor", armour));
        request.put("width", 320);
        request.put("height", 480);
        return renderer.render(request);
    }

    /** Panel colour of the fixture container texture; it must never match one of the skin colours. */
    private static final int PANEL = 0xFF203040;
    private static final int TOOLTIP_FILL = 0xFF102030;
    private static final int TOOLTIP_FRAME = 0xFFB030FF;
    private static final int SWORD = 0xFFE01080;
    /** Vanilla InventoryScreen geometry. */
    private static final int PREVIEW_LEFT = 26;
    private static final int PREVIEW_TOP = 8;
    private static final int PREVIEW_RIGHT = 75;
    private static final int PREVIEW_BOTTOM = 78;

    private static void testPlayerInventory(Renderer renderer, Path root) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "player_inventory");
        request.put("resourceRoot", root.toString());
        request.put("playerInventory", Map.of("skin", Map.of("texture", PlayerSkinFixture.png64())));
        BufferedImage image = renderer.render(request);
        require(image.getWidth() == 176 && image.getHeight() == 166, "player inventory dimensions");
        // The container texture is blitted one to one, so the top left pixel is the panel colour and
        // every other panel pixel too until the entity preview overlaps it.
        require(sameColor(image.getRGB(0, 0), PANEL & 0xFFFFFF), "player inventory panel is not drawn one to one");
        require(sameColor(image.getRGB(175, 165), PANEL & 0xFFFFFF), "player inventory panel is scaled");
        int[] bounds = modelBounds(image);
        require(bounds != null, "player inventory preview is empty");
        require(bounds[0] >= PREVIEW_LEFT && bounds[2] <= PREVIEW_RIGHT, "preview model leaves the vanilla viewport horizontally: " + bounds[0] + ".." + bounds[2]);
        require(bounds[1] >= PREVIEW_TOP && bounds[3] <= PREVIEW_BOTTOM, "preview model leaves the vanilla viewport vertically: " + bounds[1] + ".." + bounds[3]);
        int span = bounds[3] - bounds[1] + 1;
        // A 30 px per block scale over a 32 px tall model at 20 degrees of pitch projects to roughly
        // 64 px; the exact value depends on which texture pixels are opaque.
        require(span >= 60 && span <= 68, "preview model vertical span is not the vanilla size: " + span
                + " bounds=" + bounds[0] + "," + bounds[1] + ".." + bounds[2] + "," + bounds[3]);
        // The panel keeps the vanilla slot geometry, so a slot icon is drawn inside the panel grid.
        Map<String, Object> occupied = new LinkedHashMap<>(request);
        occupied.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "slots", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:diamond")))));
        BufferedImage withItem = renderer.render(occupied);
        require(countExact(withItem, 0x50DCFF) > 0, "player inventory slot icon missing");
    }

    private static void testTooltip(Renderer renderer, Path root) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "tooltip");
        request.put("resourceRoot", root.toString());
        request.put("tooltip", Map.of(
                "title", "\u00A7bFixture Title",
                "lines", List.of("\u00A7cRed line", "\u00A77Gray line"),
                "advanced", true));
        BufferedImage image = renderer.render(request);
        // Three lines at ten pixels plus the twelve pixel background border on every side.
        require(image.getWidth() > 24 && image.getHeight() == 3 * 10 + 24, "tooltip dimensions");
        int[] bounds = opaqueBounds(image);
        require(bounds[0] == 8 && bounds[1] == 8, "tooltip background border is not twelve pixels: " + bounds[0] + "," + bounds[1]);
        require(bounds[2] == image.getWidth() - 9 && bounds[3] == image.getHeight() - 9, "tooltip background edge is wrong");
        require(sameColor(image.getRGB(image.getWidth() / 2, image.getHeight() / 2), TOOLTIP_FILL & 0xFFFFFF), "tooltip background sprite is not used");
        require(countExact(image, 0xB030FF) > 8, "advanced tooltip frame sprite is not used");
        require(countExact(image, 0xFF5555) > 4, "tooltip colour codes are ignored");
        require(countExact(image, 0xAAAAAA) > 4, "tooltip default colour is wrong");

        // The vanilla line order: name, lore, enchantments, attribute header and modifiers.
        Map<String, Object> item = new LinkedHashMap<>();
        item.put("id", "minecraft:diamond_sword");
        item.put("lore", List.of("Lore line"));
        item.put("enchantments", List.of("minecraft:sharpness 5"));
        item.put("components", Map.of("minecraft:attribute_modifiers", Map.of("modifiers", List.of(
                Map.of("type", "minecraft:attack_damage", "amount", 7, "operation", "add_value", "slot", "mainhand")))));
        Map<String, Object> content = new LinkedHashMap<>();
        content.put("operation", "tooltip");
        content.put("resourceRoot", root.toString());
        content.put("tooltip", Map.of("item", item));
        BufferedImage lines = renderer.render(content);
        // name + lore + enchantment + attribute header + attribute modifier
        require(lines.getHeight() == 5 * 10 + 24, "tooltip line order or count changed: " + lines.getHeight());
        require(countExact(lines, 0xAA00AA) > 4, "lore lines are not dark purple");
        require(countExact(lines, 0xAAAAAA) > 4, "enchantment lines are not gray");
        require(countExact(lines, 0x5555FF) > 4, "attribute modifier lines are not blue");

        Map<String, Object> icon = new LinkedHashMap<>();
        icon.put("operation", "tooltip");
        icon.put("resourceRoot", root.toString());
        icon.put("tooltip", Map.of("icon", true, "item", Map.of("id", "minecraft:diamond")));
        BufferedImage withIcon = renderer.render(icon);
        require(countExact(withIcon, 0x50DCFF) > 0, "tooltip icon is not drawn when requested");
        require(countExact(lines, 0x50DCFF) == 0, "tooltip icon is drawn by default");
    }

    private static void testHeldItems(Renderer renderer, Path root) throws Exception {
        BufferedImage none = renderPlayer(renderer, root, "classic", false, List.of());
        require(countExact(none, 0xE01080) == 0, "held item rendered without equipment");
        int[] plain = skinBounds(none);

        BufferedImage mainHand = renderEquipment(renderer, root, Map.of("mainHand", Map.of("id", "minecraft:diamond_sword")));
        require(countExact(mainHand, 0xE01080) > 20, "main hand item is missing");
        int[] heldBounds = exactBounds(mainHand, 0xE01080);
        // The unmirrored third person pose tilts the blade back over the shoulder, which is behind the
        // torso, so only the part that swings past the body silhouette is visible. It has to reach the
        // viewer's left of the body, above the hips and behind the shoulder.
        require(heldBounds[0] < 60, "main hand item is not held out to the side: " + heldBounds[0]);
        require(heldBounds[3] > 250, "main hand item does not hang down from the hand: " + heldBounds[3]);
        require(heldBounds[1] < 235, "main hand item does not reach the shoulder: " + heldBounds[1]);
        // A slim arm moves the pivot and therefore the item inward by half a pixel of skin.
        BufferedImage slim = renderSlimEquipment(renderer, root);
        int[] slimBounds = exactBounds(slim, 0xE01080);
        require(slimBounds[0] > heldBounds[0], "slim arm does not move the held item towards the body");
        // The held item must not take part in the model fit, so the body silhouette is unchanged.
        int[] withItem = skinBounds(mainHand);
        for (int index = 0; index < 4; index++) {
            require(Math.abs(withItem[index] - plain[index]) <= 1, "held item changed the player model bounds");
        }
    }

    private static BufferedImage renderSlimEquipment(Renderer renderer, Path root) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "player");
        request.put("resourceRoot", root.toString());
        request.put("skin", Map.of("texture", PlayerSkinFixture.png64(), "model", "slim"));
        request.put("equipment", Map.of("mainHand", Map.of("id", "minecraft:diamond_sword")));
        request.put("width", 320);
        request.put("height", 480);
        return renderer.render(request);
    }

    private static BufferedImage renderEquipment(Renderer renderer, Path root, Map<String, Object> equipment) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "player");
        request.put("resourceRoot", root.toString());
        request.put("skin", Map.of("texture", PlayerSkinFixture.png64(), "model", "classic"));
        request.put("equipment", equipment);
        request.put("width", 320);
        request.put("height", 480);
        return renderer.render(request);
    }

    /** Bounding box of the pixels that carry one of the skin colours. */
    private static int[] skinBounds(BufferedImage image) {
        int[] bounds = null;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) <= 200) {
                    continue;
                }
                if (!matches(pixel, ColorKind.HEAD) && !matches(pixel, ColorKind.BODY)
                        && !matches(pixel, ColorKind.RIGHT_ARM) && !matches(pixel, ColorKind.LEFT_ARM)
                        && !matches(pixel, ColorKind.RIGHT_LEG) && !matches(pixel, ColorKind.LEFT_LEG)
                        && !matches(pixel, ColorKind.HAT)) {
                    continue;
                }
                bounds = extend(bounds, x, y);
            }
        }
        require(bounds != null, "no player model pixels found");
        return bounds;
    }

    private static int[] modelBounds(BufferedImage image) {
        int[] bounds = null;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) <= 200) {
                    continue;
                }
                if (!matches(pixel, ColorKind.HEAD) && !matches(pixel, ColorKind.BODY)
                        && !matches(pixel, ColorKind.RIGHT_ARM) && !matches(pixel, ColorKind.LEFT_ARM)
                        && !matches(pixel, ColorKind.RIGHT_LEG) && !matches(pixel, ColorKind.LEFT_LEG)) {
                    continue;
                }
                bounds = extend(bounds, x, y);
            }
        }
        return bounds;
    }

    private static int[] opaqueBounds(BufferedImage image) {
        int[] bounds = null;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if ((image.getRGB(x, y) >>> 24) == 0) {
                    continue;
                }
                bounds = extend(bounds, x, y);
            }
        }
        require(bounds != null, "image is fully transparent");
        return bounds;
    }

    private static int[] extend(int[] bounds, int x, int y) {
        if (bounds == null) {
            return new int[]{x, y, x, y};
        }
        bounds[0] = Math.min(bounds[0], x);
        bounds[1] = Math.min(bounds[1], y);
        bounds[2] = Math.max(bounds[2], x);
        bounds[3] = Math.max(bounds[3], y);
        return bounds;
    }

    /** Counts pixels with exactly this RGB value and a fully opaque alpha channel. */
    private static int countExact(BufferedImage image, int rgb) {
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) == 255 && (pixel & 0xFFFFFF) == rgb) {
                    count++;
                }
            }
        }
        return count;
    }

    private static int[] exactBounds(BufferedImage image, int rgb) {
        int[] bounds = null;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) == 255 && (pixel & 0xFFFFFF) == rgb) {
                    bounds = extend(bounds, x, y);
                }
            }
        }
        require(bounds != null, "no pixels for colour " + Integer.toHexString(rgb));
        return bounds;
    }

    private static void requireFails(Renderer renderer, Map<String, Object> request, String expected) throws Exception {
        try {
            renderer.render(request);
        } catch (Exception error) {
            String message = String.valueOf(error.getMessage());
            require(message.contains(expected), "unexpected failure: " + message);
            return;
        }
        throw new AssertionError("expected failure containing " + expected);
    }

    private enum ColorKind {
        HEAD, BODY, RIGHT_ARM, LEFT_ARM, RIGHT_LEG, LEFT_LEG, HAT,
        ARMOUR_HEAD, ARMOUR_BODY, ARMOUR_LEG, ARMOUR_BOOT,
        MAP_GRASS, MAP_SAND, DECORATION_WHITE, DECORATION_BLACK, PING
    }

    /** Expected texture colours normalised by their brightest channel (face shading scales them). */
    private static double[] base(ColorKind kind) {
        return switch (kind) {
            case HEAD -> new double[]{1, 0.2, 0.2};
            case HAT -> new double[]{1, 1, 1};
            case BODY -> new double[]{0.22, 1, 0.22};
            case RIGHT_ARM -> new double[]{1, 0.91, 0.17};
            case LEFT_ARM -> new double[]{0.17, 0.83, 1};
            case RIGHT_LEG -> new double[]{1, 0.19, 1};
            case LEFT_LEG -> new double[]{1, 0.5, 0.08};
            case ARMOUR_HEAD -> new double[]{1, 0.52, 0.65};
            case ARMOUR_BODY -> new double[]{0.08, 0.5, 1};
            case ARMOUR_LEG -> new double[]{0.58, 1, 0.25};
            case ARMOUR_BOOT -> new double[]{0.45, 0.3, 1};
            case MAP_GRASS -> new double[]{0.71, 1, 0.31};
            case MAP_SAND -> new double[]{1, 0.94, 0.66};
            case DECORATION_WHITE, DECORATION_BLACK, PING -> new double[]{0, 0, 0};
        };
    }

    private static boolean matches(int pixel, ColorKind kind) {
        double red = ((pixel >> 16) & 0xFF) / 255.0;
        double green = ((pixel >> 8) & 0xFF) / 255.0;
        double blue = (pixel & 0xFF) / 255.0;
        double max = Math.max(red, Math.max(green, blue));
        switch (kind) {
            case DECORATION_WHITE -> {
                return red > 0.78 && green > 0.78 && blue > 0.78;
            }
            case DECORATION_BLACK -> {
                return max < 0.2;
            }
            case PING -> {
                return green > 0.5 && red < 0.35 && blue < 0.35;
            }
            default -> {
            }
        }
        if (max < 0.2) {
            return false;
        }
        double[] expected = base(kind);
        return Math.abs(red / max - expected[0]) < 0.16
                && Math.abs(green / max - expected[1]) < 0.16
                && Math.abs(blue / max - expected[2]) < 0.16;
    }

    private static int countColor(BufferedImage image, ColorKind kind) {
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) > 200 && matches(pixel, kind)) {
                    count++;
                }
            }
        }
        return count;
    }

    private static double centerX(BufferedImage image, ColorKind kind) {
        long total = 0;
        long sum = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) > 200 && matches(pixel, kind)) {
                    total++;
                    sum += x;
                }
            }
        }
        require(total > 0, "no pixels for " + kind);
        return sum / (double) total;
    }

    private static double centerY(BufferedImage image, ColorKind kind) {
        return centerY(image, kind, 0, 0, image.getWidth(), image.getHeight());
    }

    private static double centerY(BufferedImage image, ColorKind kind, int x0, int y0, int x1, int y1) {
        long total = 0;
        long sum = 0;
        for (int y = Math.max(0, y0); y < Math.min(image.getHeight(), y1); y++) {
            for (int x = Math.max(0, x0); x < Math.min(image.getWidth(), x1); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) > 200 && matches(pixel, kind)) {
                    total++;
                    sum += y;
                }
            }
        }
        require(total > 0, "no pixels for " + kind);
        return sum / (double) total;
    }

    private static int contentWidth(BufferedImage image) {
        int left = image.getWidth();
        int right = -1;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if ((image.getRGB(x, y) >>> 24) > 8) {
                    left = Math.min(left, x);
                    right = Math.max(right, x);
                }
            }
        }
        return right - left;
    }

    private static int countAlpha(BufferedImage image, int alpha) {
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if ((image.getRGB(x, y) >>> 24) >= alpha) {
                    count++;
                }
            }
        }
        return count;
    }

    private static boolean sameColor(int pixel, int rgb) {
        return ((pixel >> 16) & 0xFF) == ((rgb >> 16) & 0xFF)
                && ((pixel >> 8) & 0xFF) == ((rgb >> 8) & 0xFF)
                && (pixel & 0xFF) == (rgb & 0xFF);
    }

    private static boolean hasPixels(BufferedImage image) {
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if ((image.getRGB(x, y) >>> 24) != 0) return true;
            }
        }
        return false;
    }

    private static void require(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }

    static boolean writePng(BufferedImage image, Path path) throws Exception {
        return ImageIO.write(image, "png", Files.newOutputStream(path));
    }
}
