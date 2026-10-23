package com.illtamer.minecraft;

import javax.imageio.ImageIO;
import java.awt.Graphics2D;
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
        testContainers(renderer, root);
        testPlayerInventory(renderer, root);
        testNativePixelScale(renderer, root);
        testAchievement(renderer, root);
        testTooltip(renderer, root);
        testHeldItems(renderer, root);
        testTextColours(renderer, root);
        testHexColours(renderer, root);
        testWrappedSpaces(renderer, root);
        testBoldGlyphs(renderer, root);
        testBlockIcon(renderer, root);
        testChineseFont(renderer, root);
        testMissingFontDefinition(root);
        testSpecialItems(renderer, root);
        testSpecialComponents(renderer, root);
        testSpecialHeldItems(renderer, root);
        testSpecialItemList(renderer, root);
        testCustomArt(renderer, root);
        testMissingResources(renderer, root);
        System.out.println("renderer smoke: item/skin/armour/map/playerlist/container/inventory/scale/achievement/tooltip/hand/text/special/custom/resources paths passed");
    }

    private static void testItem(Renderer renderer, Path root) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "item");
        request.put("resourceRoot", root.toString());
        request.put("item", Map.of("id", "minecraft:diamond"));
        BufferedImage icon = renderer.render(request);
        require(icon.getWidth() == 128 && icon.getHeight() == 128, "item dimensions");
        require(hasPixels(icon), "item pixels");
        require(java.nio.file.Files.isRegularFile(root.resolve("assets/minecraft/font/default.json")),
                "the fixture must ship the vanilla font definition the renderer loads");
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
    /** Vanilla {@code AbstractContainerScreen} label colour. */
    private static final int LABEL_RGB = 0x404040;
    /** Fixture chest background: the slot band above and the player inventory band below row 126. */
    private static final int CHEST_TOP = 0xFF304020;
    private static final int CHEST_BOTTOM = 0xFF402030;
    private static final int TOOLTIP_FILL = 0xFF102030;
    private static final int TOOLTIP_FRAME = 0xFFB030FF;
    private static final int SWORD = 0xFFE01080;
    /** Vanilla InventoryScreen geometry. */
    private static final int PREVIEW_LEFT = 26;
    private static final int PREVIEW_TOP = 8;
    private static final int PREVIEW_RIGHT = 75;
    private static final int PREVIEW_BOTTOM = 78;

    private static void testContainers(Renderer renderer, Path root) throws Exception {
        // ContainerScreen blits two bands of generic_54: the slot band grows with the row count while
        // the 96 px player inventory band below it is always the same slice of the source texture.
        BufferedImage chest = renderContainer(renderer, root, "inventory", Map.of("title", "Chest", "rows", 3));
        require(chest.getWidth() == 176 && chest.getHeight() == 167, "chest dimensions: " + chest.getWidth() + "x" + chest.getHeight());
        require(sameColor(chest.getRGB(0, 0), CHEST_TOP), "chest slot band is not blitted from the texture top");
        require(sameColor(chest.getRGB(0, 70), CHEST_TOP), "chest slot band is too short");
        require(sameColor(chest.getRGB(0, 71), CHEST_BOTTOM), "chest player band does not start at rows * 18 + 17");
        require(sameColor(chest.getRGB(0, 166), CHEST_BOTTOM), "chest player band does not reach the canvas bottom");
        // AbstractContainerScreen draws every title at x 8 on row 6 with a seven pixel glyph box.
        int[] title = exactBounds(chest, 0x404040);
        require(title[0] == 8 && title[1] == 6, "container title is not at the vanilla label position: " + title[0] + "," + title[1]);
        require(title[3] == 12, "container title glyph box is not seven pixels tall: " + title[3]);
        BufferedImage sixRows = renderContainer(renderer, root, "inventory", Map.of("rows", 6));
        require(sixRows.getHeight() == 6 * 18 + 113, "six row chest dimensions: " + sixRows.getHeight());

        // Menu slot coordinates: slot 0 of a chest sits at (8, 18) and the fixture diamond only has
        // opaque pixels inside (2, 2)..(13, 13) of its 16 px icon.
        BufferedImage empty = renderContainer(renderer, root, "inventory", Map.of("rows", 3));
        BufferedImage one = renderContainer(renderer, root, "inventory", Map.of("rows", 3,
                "slots", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:diamond")))));
        int[] icon = diffBounds(empty, one);
        require(icon[0] == 10 && icon[1] == 20 && icon[2] == 21 && icon[3] == 31,
                "slot 0 icon is not at the vanilla slot coordinate: " + java.util.Arrays.toString(icon));
        // The player inventory block starts 13 px below the slot band and its hotbar 58 px below that,
        // so with 27 container slots the first hotbar slot is menu slot 54 at (8, 143).
        BufferedImage hotbar = renderContainer(renderer, root, "inventory", Map.of("rows", 3,
                "slots", List.of(Map.of("slot", 54, "item", Map.of("id", "minecraft:diamond")))));
        int[] firstHotbar = diffBounds(empty, hotbar);
        require(firstHotbar[0] == 10 && firstHotbar[1] == 145,
                "hotbar slot is not at the vanilla player inventory coordinate: " + java.util.Arrays.toString(firstHotbar));

        // renderItemDecorations puts the stack size in the lower right corner of the slot, right
        // aligned two pixels above the slot border; the shadow extends it by one more pixel.
        BufferedImage stacked = renderContainer(renderer, root, "inventory", Map.of("rows", 3,
                "slots", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:diamond", "count", 64)))));
        int[] count = diffBounds(one, stacked);
        require(count[0] == 13 && count[1] == 27 && count[2] == 24 && count[3] == 34,
                "stack size is not at the vanilla decoration position: " + java.util.Arrays.toString(count));

        // The ender chest is the very same screen: same texture, same colours, no tint of its own.
        BufferedImage ender = renderContainer(renderer, root, "enderchest", Map.of("rows", 3));
        require(ender.getWidth() == chest.getWidth() && ender.getHeight() == chest.getHeight(), "ender chest dimensions");
        require(equalPixels(empty, ender), "ender chest does not match a plain three row container");

        // HopperScreen is 133 px tall and its five slots start at x 44 on row 20.
        BufferedImage hopper = renderContainer(renderer, root, "item_container", Map.of("container", "hopper"));
        require(hopper.getWidth() == 176 && hopper.getHeight() == 133, "hopper dimensions: " + hopper.getWidth() + "x" + hopper.getHeight());
        // Every other screen is a single 176x166 blit of its own texture.
        for (String kind : List.of("furnace", "blast_furnace", "smoker", "crafting_table", "anvil", "brewing_stand")) {
            BufferedImage screen = renderContainer(renderer, root, "item_container", Map.of("container", kind));
            require(screen.getWidth() == 176 && screen.getHeight() == 166, kind + " dimensions: " + screen.getWidth() + "x" + screen.getHeight());
        }
        // Fixed menu layouts, straight from the decompiled screens: the result slots of the furnace,
        // anvil and crafting table are the ones the previous hand tuned table got wrong.
        requireSlot(renderer, root, "hopper", 0, 44, 20);
        requireSlot(renderer, root, "hopper", 5, 8, 51);
        requireSlot(renderer, root, "furnace", 1, 56, 53);
        requireSlot(renderer, root, "furnace", 2, 116, 35);
        requireSlot(renderer, root, "furnace", 3, 8, 84);
        requireSlot(renderer, root, "crafting_table", 0, 30, 17);
        requireSlot(renderer, root, "crafting_table", 9, 124, 35);
        requireSlot(renderer, root, "anvil", 2, 134, 47);
        requireSlot(renderer, root, "brewing_stand", 3, 79, 17);
        requireSlot(renderer, root, "brewing_stand", 4, 17, 17);
        Map<String, Object> unsupported = new LinkedHashMap<>();
        unsupported.put("operation", "item_container");
        unsupported.put("resourceRoot", root.toString());
        unsupported.put("container", Map.of("container", "not_a_container"));
        requireFails(renderer, unsupported, "unsupported_container");
    }

    /** Asserts the menu slot coordinate by diffing the icon the renderer blits into the slot. */
    private static void requireSlot(Renderer renderer, Path root, String kind, int slot, int x, int y) throws Exception {
        BufferedImage empty = renderContainer(renderer, root, "item_container", Map.of("container", kind));
        BufferedImage filled = renderContainer(renderer, root, "item_container", Map.of("container", kind,
                "slots", List.of(Map.of("slot", slot, "item", Map.of("id", "minecraft:diamond")))));
        int[] bounds = diffBounds(empty, filled);
        // The fixture diamond is opaque inside (2, 2)..(13, 13) of its 16 px icon.
        require(bounds[0] == x + 2 && bounds[1] == y + 2,
                kind + " slot " + slot + " is not at the vanilla menu coordinate: " + java.util.Arrays.toString(bounds));
    }

    private static BufferedImage renderContainer(Renderer renderer, Path root, String operation, Map<String, Object> container) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", operation);
        request.put("resourceRoot", root.toString());
        request.put("container", container);
        return renderer.render(request);
    }

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
        // InventoryScreen overrides titleLabelX to 97 so the title clears the recipe book grid.
        Map<String, Object> titled = new LinkedHashMap<>(request);
        titled.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "title", "Chest"));
        int[] title = exactBounds(renderer.render(titled), 0x404040);
        require(title[0] == 97 && title[1] == 6, "player inventory title is not at the vanilla label position: " + title[0] + "," + title[1]);
        require(title[3] == 12, "player inventory title glyph box is not seven pixels tall: " + title[3]);

        // InventoryMenu slot coordinates: slot 0 sits at (8, 84) and the hotbar starts at (8, 142).
        Map<String, Object> occupied = new LinkedHashMap<>(request);
        occupied.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "slots", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:diamond")))));
        BufferedImage withItem = renderer.render(occupied);
        require(countExact(withItem, 0x50DCFF) > 0, "player inventory slot icon missing");
        int[] backpack = diffBounds(image, withItem);
        require(backpack[0] == 10 && backpack[1] == 86,
                "backpack slot is not at the vanilla menu coordinate: " + java.util.Arrays.toString(backpack));
        Map<String, Object> hotbarRequest = new LinkedHashMap<>(request);
        hotbarRequest.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "slots", List.of(Map.of("slot", 27, "item", Map.of("id", "minecraft:diamond")))));
        int[] hotbar = diffBounds(image, renderer.render(hotbarRequest));
        require(hotbar[0] == 10 && hotbar[1] == 144,
                "hotbar slot is not at the vanilla InventoryMenu coordinate: " + java.util.Arrays.toString(hotbar));

        // The stack size keeps the vanilla decoration position inside the slot.
        Map<String, Object> stackedRequest = new LinkedHashMap<>(request);
        stackedRequest.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "slots", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:diamond", "count", 64)))));
        int[] count = diffBounds(withItem, renderer.render(stackedRequest));
        require(count[0] == 13 && count[1] == 93 && count[2] == 24 && count[3] == 100,
                "player inventory stack size is not at the vanilla decoration position: " + java.util.Arrays.toString(count));

        // The armour column keeps the InventoryMenu coordinates (8, 8 + index * 18) and the offhand
        // slot sits at (77, 62), which is the last slot of the vanilla inventory menu.
        Map<String, Object> offhandRequest = new LinkedHashMap<>(request);
        offhandRequest.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "offhand", Map.of("id", "minecraft:diamond")));
        BufferedImage withOffhand = renderer.render(offhandRequest);
        require(diffCountIn(image, withOffhand, 77, 62, 93, 78) > 50, "offhand slot icon missing");
        require(diffCountIn(image, withOffhand, 0, 0, 26, 84) == 0, "offhand icon is drawn outside its slot");

        // The held item hangs at hand height in front of the preview model, inside the vanilla
        // viewport, and it must not rescale that model: the preview viewport is fixed.
        Map<String, Object> heldRequest = new LinkedHashMap<>(request);
        heldRequest.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "mainHand", Map.of("id", "minecraft:diamond_sword")));
        BufferedImage held = renderer.render(heldRequest);
        int heldSword = countExact(held, SWORD & 0xFFFFFF);
        require(heldSword > 20, "player inventory preview is missing the held item");
        int[] heldItem = exactBounds(held, SWORD & 0xFFFFFF);
        require(heldItem[0] >= PREVIEW_LEFT && heldItem[2] <= PREVIEW_RIGHT
                        && heldItem[1] >= PREVIEW_TOP && heldItem[3] <= PREVIEW_BOTTOM,
                "held item leaves the vanilla preview viewport: " + java.util.Arrays.toString(heldItem));
        int overPreview = overModelPixels(image, held, SWORD & 0xFFFFFF, PANEL);
        require(overPreview * 2 >= heldSword, "held item is not drawn in front of the preview model: "
                + overPreview + "/" + heldSword);
        int[] previewBounds = modelBounds(held);
        for (int index = 0; index < 4; index++) {
            require(Math.abs(previewBounds[index] - bounds[index]) <= 1,
                    "held item changed the vanilla preview bounds: " + java.util.Arrays.toString(previewBounds));
        }
    }

    /**
     * A whole render scale draws the same layout at the final resolution instead of magnifying the
     * native picture. The panel sprites keep being one texel per render scale pixels, while the
     * preview model and the labels are rasterised again at that resolution — which is what stops a
     * magnified skin from turning into a blocky upscale of the 176x166 render.
     */
    private static void testNativePixelScale(Renderer renderer, Path root) throws Exception {
        Map<String, Object> plain = new LinkedHashMap<>();
        plain.put("operation", "player_inventory");
        plain.put("resourceRoot", root.toString());
        plain.put("playerInventory", Map.of("skin", Map.of("texture", PlayerSkinFixture.png64()), "title", "\u4E2D\u6587"));
        BufferedImage one = renderer.render(plain);
        require(one.getWidth() == 176 && one.getHeight() == 166, "native player inventory dimensions");

        Map<String, Object> quadrupled = new LinkedHashMap<>(plain);
        quadrupled.put("pixelScale", 4);
        BufferedImage four = renderer.render(quadrupled);
        require(four.getWidth() == 704 && four.getHeight() == 664,
                "quadrupled player inventory is " + four.getWidth() + "x" + four.getHeight());

        // A panel window no sprite of the screen decorates: every device pixel must be the texel of
        // the native render it covers, which is both the block test and the no-seam test.
        for (int y = 600; y < 664; y++) {
            for (int x = 560; x < 704; x++) {
                int want = one.getRGB(x / 4, y / 4);
                if (four.getRGB(x, y) != want) {
                    throw new AssertionError("panel texel at " + x + "," + y + " is not the scaled native one");
                }
            }
        }

        // The preview model and the label, on the other hand, must not be the magnified native
        // pixels: they carry detail the 176x166 render does not have.
        int detail = previewDifference(one, four, 4);
        require(detail > 500, "the preview model was not rasterised at the render scale: " + detail + " pixels changed");

        int[] oneBounds = modelBounds(one);
        int[] fourBounds = modelBounds(four);
        require(oneBounds != null && fourBounds != null, "the preview model is missing");
        for (int index = 0; index < 4; index++) {
            require(Math.abs(fourBounds[index] - oneBounds[index] * 4) <= 4,
                    "the preview model is not at the render scale: " + java.util.Arrays.toString(fourBounds)
                            + " against " + java.util.Arrays.toString(oneBounds));
        }

        // Chinese ink is texel exact at twice the size, so the title of a scaled container carries
        // whole pixels of the label colour where the native render only had partly covered ones.
        Map<String, Object> container = new LinkedHashMap<>();
        container.put("operation", "inventory");
        container.put("resourceRoot", root.toString());
        container.put("container", Map.of("title", "\u4E2D\u6587\u6D4B\u8BD5", "rows", 3, "slots", List.of()));
        // Text is laid out in layout coordinates and only scaled by the canvas transform, so a
        // magnified label keeps its advance: the ink of the same glyphs must sit exactly twice as far
        // apart instead of growing inside its own cell and overlapping the next one.
        Map<String, Object> text = new LinkedHashMap<>();
        text.put("operation", "tooltip");
        text.put("resourceRoot", root.toString());
        text.put("tooltip", Map.of("title", "Scale Test"));
        BufferedImage textOne = renderer.render(text);
        int[] textInkOne = exactBounds(textOne, 0xFFFFFF);
        Map<String, Object> doubledText = new LinkedHashMap<>(text);
        doubledText.put("pixelScale", 2);
        BufferedImage textTwo = renderer.render(doubledText);
        int[] textInkTwo = exactBounds(textTwo, 0xFFFFFF);
        for (int index = 0; index < 4; index++) {
            int want = textInkOne[index] * 2;
            require(Math.abs(textInkTwo[index] - want) <= 2,
                    "the doubled label ink moved to " + java.util.Arrays.toString(textInkTwo)
                            + ", want twice " + java.util.Arrays.toString(textInkOne));
        }

        // Four full width Chinese cells advance 36 px, so the doubled title starts on column 18 and
        // must stay inside its own 72 px: a glyph drawn twice as large as its cell spills past it.
        Map<String, Object> doubledTitle = new LinkedHashMap<>(container);
        doubledTitle.put("pixelScale", 2);
        int[] doubledLabel = exactBounds(renderer.render(doubledTitle), LABEL_RGB);
        require(doubledLabel[0] == 18, "the doubled container title left its column: " + doubledLabel[0]);
        require(doubledLabel[2] <= 18 + 72, "the doubled container title overlaps its own cells: " + doubledLabel[2]);
        require(doubledLabel[3] - doubledLabel[1] + 1 <= 16,
                "the doubled container title is taller than its eight pixel font cell: "
                        + (doubledLabel[3] - doubledLabel[1] + 1));
        int nativeInk = countExact(renderer.render(container), LABEL_RGB);
        Map<String, Object> doubledContainer = new LinkedHashMap<>(container);
        doubledContainer.put("pixelScale", 2);
        int scaledInk = countExact(renderer.render(doubledContainer), LABEL_RGB);
        require(scaledInk > 4 * Math.max(1, nativeInk),
                "the doubled container title is not texel exact: " + scaledInk + " solid pixels against " + nativeInk);
    }

    /**
     * Counts the pixels of the preview viewport that differ between a native render and a render at
     * {@code scale}, which is how foreign detail is told apart from a magnified native picture.
     */
    private static int previewDifference(BufferedImage one, BufferedImage scaled, int scale) {
        int difference = 0;
        for (int y = PREVIEW_TOP * scale; y < Math.min(scaled.getHeight(), PREVIEW_BOTTOM * scale); y++) {
            for (int x = PREVIEW_LEFT * scale; x < Math.min(scaled.getWidth(), PREVIEW_RIGHT * scale); x++) {
                if (scaled.getRGB(x, y) != one.getRGB(x / scale, y / scale)) {
                    difference++;
                }
            }
        }
        return difference;
    }

    private static void testAchievement(Renderer renderer, Path root) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "achievement");
        request.put("resourceRoot", root.toString());
        request.put("achievement", Map.of(
                "title", "Fixture", "description", "Done", "frame", "task", "completed", true,
                "icon", Map.of("id", "minecraft:diamond")));
        BufferedImage image = renderer.render(request);
        require(image.getWidth() == 480 && image.getHeight() == 96, "achievement dimensions");
        // AdvancementToast draws its texts at x 30 on rows 7 and 18 of the 160x32 toast, so the
        // default 480x96 output puts the title at three times that position.
        int[] title = exactBounds(image, 0xFFFF55);
        require(title[0] >= 90 && title[0] <= 96, "achievement title is not at the vanilla column: " + title[0]);
        require(title[1] >= 21 && title[1] <= 27, "achievement title is not on the vanilla row: " + title[1]);
        int[] description = exactBounds(image, 0xFFFFFF);
        require(description[1] >= 54 && description[1] <= 60, "achievement description is not on the vanilla row: " + description[1]);
    }

    private static void testTooltip(Renderer renderer, Path root) throws Exception {
        Map<String, Object> item = new LinkedHashMap<>();
        item.put("id", "minecraft:diamond_sword");
        item.put("lore", List.of("\u00A7cRed line", "\u00A77Gray line"));
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "tooltip");
        request.put("resourceRoot", root.toString());
        request.put("tooltip", Map.of(
                "title", "\u00A7bFixture Title",
                "item", item,
                "advanced", true));
        BufferedImage image = renderer.render(request);
        // Title, advanced item id, the two lore lines and the advanced component count: five lines at
        // ten pixels plus the twelve pixel background border on every side.
        require(image.getWidth() > 24 && image.getHeight() == 5 * 10 + 24, "tooltip dimensions");
        int[] bounds = opaqueBounds(image);
        require(bounds[0] == 8 && bounds[1] == 8, "tooltip background border is not twelve pixels: " + bounds[0] + "," + bounds[1]);
        require(bounds[2] == image.getWidth() - 9 && bounds[3] == image.getHeight() - 9, "tooltip background edge is wrong");
        require(sameColor(image.getRGB(image.getWidth() / 2, image.getHeight() / 2), TOOLTIP_FILL & 0xFFFFFF), "tooltip background sprite is not used");
        require(countExact(image, 0xB030FF) > 8, "advanced tooltip frame sprite is not used");
        require(countExact(image, 0xFF5555) > 4, "tooltip colour codes are ignored");
        require(countExact(image, 0xAAAAAA) > 4, "tooltip default colour is wrong");

        // The vanilla line order: name, lore, enchantments, attribute header and modifiers. Lore is
        // the only free form line source and always comes from the item.
        Map<String, Object> contentItem = new LinkedHashMap<>();
        contentItem.put("id", "minecraft:diamond_sword");
        contentItem.put("lore", List.of("Lore line"));
        contentItem.put("enchantments", List.of("minecraft:sharpness 5"));
        contentItem.put("components", Map.of("minecraft:attribute_modifiers", Map.of("modifiers", List.of(
                Map.of("type", "minecraft:attack_damage", "amount", 7, "operation", "add_value", "slot", "mainhand")))));
        Map<String, Object> content = new LinkedHashMap<>();
        content.put("operation", "tooltip");
        content.put("resourceRoot", root.toString());
        content.put("tooltip", Map.of("item", contentItem));
        BufferedImage lines = renderer.render(content);
        // name + lore + enchantment + attribute header + attribute modifier
        require(lines.getHeight() == 5 * 10 + 24, "tooltip line order or count changed: " + lines.getHeight());
        require(countExact(lines, 0xAA00AA) > 4, "lore lines are not dark purple");
        require(countExact(lines, 0xAAAAAA) > 4, "enchantment lines are not gray");
        require(countExact(lines, 0x5555FF) > 4, "attribute modifier lines are not blue");

        // A tooltip without an item has no lore source and only the title line.
        Map<String, Object> titleOnly = new LinkedHashMap<>();
        titleOnly.put("operation", "tooltip");
        titleOnly.put("resourceRoot", root.toString());
        titleOnly.put("tooltip", Map.of("title", "Fixture Title"));
        require(renderer.render(titleOnly).getHeight() == 1 * 10 + 24 - 2, "title only tooltip dimensions");

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
        require(countExact(none, SWORD & 0xFFFFFF) == 0, "held item rendered without equipment");

        BufferedImage mainHand = renderEquipment(renderer, root, Map.of("mainHand", Map.of("id", "minecraft:diamond_sword")));
        int mainSword = countExact(mainHand, SWORD & 0xFFFFFF);
        require(mainSword > 20, "main hand item is missing");
        // The vanilla ItemInHandLayer pose hangs the item at hand height in front of the torso, so
        // every visible item pixel has to be drawn over the body instead of behind its back.
        int mainOverBody = overModelPixels(none, mainHand, SWORD & 0xFFFFFF, -1);
        require(mainOverBody * 2 >= mainSword, "main hand item is not drawn in front of the torso: "
                + mainOverBody + "/" + mainSword);
        int[] heldBounds = exactBounds(mainHand, SWORD & 0xFFFFFF);
        int[] bodyBounds = skinBounds(mainHand);
        require(heldBounds[0] >= bodyBounds[0] && heldBounds[2] <= bodyBounds[2],
                "main hand item is held outside the body silhouette: " + java.util.Arrays.toString(heldBounds)
                        + " body=" + java.util.Arrays.toString(bodyBounds));
        // The item belongs to the figure the fit is computed from and must stay inside the canvas.
        require(heldBounds[0] > 0 && heldBounds[2] < mainHand.getWidth() - 1
                        && heldBounds[1] > 0 && heldBounds[3] < mainHand.getHeight() - 1,
                "main hand item is clipped by the canvas: " + java.util.Arrays.toString(heldBounds));
        require(heldBounds[1] > (bodyBounds[1] + bodyBounds[3]) / 2,
                "main hand item is not held at hand height: " + java.util.Arrays.toString(heldBounds));

        // The offhand runs through the mirrored display transform and lands in front of the torso
        // too. Its item hangs past the far arm in this three quarter view, so only part of the rod
        // overlaps the body, while a version rendered behind the back would be occluded instead.
        BufferedImage offhand = renderEquipment(renderer, root, Map.of("offhand", Map.of("id", "minecraft:diamond_sword")));
        int offhandSword = countExact(offhand, SWORD & 0xFFFFFF);
        require(offhandSword > 20, "offhand item is missing");
        int offhandOverBody = overModelPixels(none, offhand, SWORD & 0xFFFFFF, -1);
        require(offhandOverBody * 3 >= offhandSword, "offhand item is not drawn in front of the torso: "
                + offhandOverBody + "/" + offhandSword);
        int[] offhandBounds = exactBounds(offhand, SWORD & 0xFFFFFF);
        require(offhandBounds[0] > 0 && offhandBounds[2] < offhand.getWidth() - 1,
                "offhand item is clipped by the canvas: " + java.util.Arrays.toString(offhandBounds));

        // A slim arm moves the pivot and therefore the item inward by half a pixel of skin.
        BufferedImage slim = renderSlimEquipment(renderer, root);
        int[] slimBounds = exactBounds(slim, SWORD & 0xFFFFFF);
        require(slimBounds[0] > heldBounds[0], "slim arm does not move the held item towards the body");
    }

    /**
     * A custom item replaces the vanilla icon of its base material: the request carries the model and
     * the textures of the item a base material plus a custom model data value resolve to, and the
     * renderer has to draw that model — as a flat sprite, as geometry, in a hand, and inside a tooltip —
     * while a value that was not resolved keeps the vanilla item.
     */
    private static void testCustomArt(Renderer renderer, Path root) throws Exception {
        int starfish = 0xFF3FA7C8;
        int crate = 0xFFC8A13F;
        String sprite = solidPNG(starfish);

        Map<String, Object> flatModel = Map.of(
                "parent", "minecraft:item/generated",
                "textures", Map.of("layer0", "test:item/starfish"));
        Map<String, Object> flatArt = Map.of(
                "model", flatModel,
                "textures", Map.of("test:item/starfish", sprite));

        // The icon comes from the inline texture, not from the vanilla paper model.
        Map<String, Object> icon = new LinkedHashMap<>();
        icon.put("operation", "item");
        icon.put("resourceRoot", root.toString());
        icon.put("item", Map.of("id", "minecraft:diamond", "customData", 1134));
        icon.put("customArt", Map.of("DIAMOND#1134", flatArt));
        icon.put("width", 64);
        icon.put("height", 64);
        BufferedImage custom = renderer.render(icon);
        require(countExact(custom, starfish & 0xFFFFFF) > 400, "the custom icon texture was not drawn");
        require(countExact(custom, starfish & 0xFFFFFF) != countExact(renderer.render(vanillaIcon(root)), starfish & 0xFFFFFF),
                "the custom icon is indistinguishable from the vanilla one");

        // A model with elements is drawn as geometry in the vanilla block pose, so its silhouette
        // leaves the canvas corners empty instead of filling the whole sprite and its three visible
        // faces carry the block face shading.
        Map<String, Object> faces = new LinkedHashMap<>();
        for (String side : List.of("north", "south", "east", "west", "up", "down")) {
            faces.put(side, Map.of("texture", "#0", "uv", List.of(0, 0, 16, 16)));
        }
        Map<String, Object> crateModel = Map.of(
                "textures", Map.of("0", "test:bxtp/crate"),
                "display", Map.of("gui", Map.of(
                        "rotation", List.of(30, 225, 0),
                        "scale", List.of(0.625, 0.625, 0.625))),
                "elements", List.of(Map.of(
                        "from", List.of(0, 0, 0),
                        "to", List.of(16, 16, 16),
                        "faces", faces)));
        Map<String, Object> crateArt = Map.of(
                "model", crateModel,
                "textures", Map.of("test:bxtp/crate", solidPNG(crate)));
        Map<String, Object> geometry = new LinkedHashMap<>(icon);
        geometry.put("item", Map.of("id", "minecraft:diamond", "customData", 10115));
        geometry.put("customArt", Map.of("DIAMOND#10115", crateArt));
        BufferedImage crateImage = renderer.render(geometry);
        require(countExact(crateImage, crate & 0xFFFFFF) > 200, "the lit top of the custom geometry was not drawn");
        require(countExact(crateImage, shaded(crate, 0.8)) > 100, "the custom geometry has no north face shade");
        require(countExact(crateImage, shaded(crate, 0.6)) > 100, "the custom geometry has no east face shade");
        require((crateImage.getRGB(0, 0) >>> 24) == 0, "the geometry fills the whole canvas, it is not a model");

        // The hand slots resolve the same item, which is the whole point of shipping model geometry.
        Map<String, Object> hand = new LinkedHashMap<>();
        hand.put("operation", "player");
        hand.put("resourceRoot", root.toString());
        hand.put("skin", Map.of("texture", PlayerSkinFixture.png64(), "model", "classic"));
        hand.put("equipment", Map.of("mainHand", Map.of("id", "minecraft:diamond", "customData", 1134)));
        hand.put("customArt", Map.of("DIAMOND#1134", flatArt));
        hand.put("width", 320);
        hand.put("height", 480);
        require(countExact(renderer.render(hand), starfish & 0xFFFFFF) > 20, "the custom item is missing from the hand");
        hand.remove("customArt");
        require(countExact(renderer.render(hand), starfish & 0xFFFFFF) == 0,
                "the custom item was drawn without custom art");

        // A tooltip icon goes through the same selection, and a value the index did not resolve keeps
        // the vanilla item instead of failing the render.
        Map<String, Object> tooltip = new LinkedHashMap<>();
        tooltip.put("operation", "tooltip");
        tooltip.put("resourceRoot", root.toString());
        tooltip.put("tooltip", Map.of(
                "icon", true,
                "item", Map.of("id", "minecraft:diamond", "customData", 1134, "name", "Starfish")));
        tooltip.put("customArt", Map.of("DIAMOND#1134", flatArt));
        require(countExact(renderer.render(tooltip), starfish & 0xFFFFFF) > 20, "the tooltip icon is not the custom item");

        Map<String, Object> unresolved = new LinkedHashMap<>(icon);
        unresolved.put("item", Map.of("id", "minecraft:diamond", "customData", 999999));
        require(countExact(renderer.render(unresolved), starfish & 0xFFFFFF) == 0,
                "an unresolved customData still drew the custom texture");
        require(hasPixels(renderer.render(unresolved)), "an unresolved customData left the icon empty");
    }

    /** Renders one item icon at the given square size. */
    private static BufferedImage itemIcon(Renderer renderer, Path root, String id, int size) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "item");
        request.put("resourceRoot", root.toString());
        request.put("item", Map.of("id", id));
        request.put("width", size);
        return renderer.render(request);
    }

    /** The centre of gravity of one exact colour, in pixels. */
    private static double[] centre(BufferedImage image, int rgb) {
        double sumX = 0;
        double sumY = 0;
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) == 255 && (pixel & 0xFFFFFF) == rgb) {
                    sumX += x;
                    sumY += y;
                    count++;
                }
            }
        }
        require(count > 0, "no pixels for colour " + Integer.toHexString(rgb));
        return new double[]{sumX / count, sumY / count};
    }

    /** One channel of a face colour after the vanilla block shade of that face. */
    private static int shaded(int rgb, double light) {
        int red = (int) Math.round((rgb >> 16 & 0xFF) * light);
        int green = (int) Math.round((rgb >> 8 & 0xFF) * light);
        int blue = (int) Math.round((rgb & 0xFF) * light);
        return red << 16 | green << 8 | blue;
    }

    private static Map<String, Object> vanillaIcon(Path root) {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "item");
        request.put("resourceRoot", root.toString());
        request.put("item", Map.of("id", "minecraft:diamond"));
        request.put("width", 64);
        request.put("height", 64);
        return request;
    }

    /** A solid colour PNG as the data URI the Go service inlines for a custom texture. */
    private static String solidPNG(int argb) throws Exception {
        BufferedImage image = new BufferedImage(16, 16, BufferedImage.TYPE_INT_ARGB);
        for (int y = 0; y < 16; y++) {
            for (int x = 0; x < 16; x++) {
                image.setRGB(x, y, argb);
            }
        }
        java.io.ByteArrayOutputStream buffer = new java.io.ByteArrayOutputStream();
        ImageIO.write(image, "png", buffer);
        return "data:image/png;base64," + java.util.Base64.getEncoder().encodeToString(buffer.toByteArray());
    }

    /**
     * Every text surface of the service accepts the vanilla {@code §} colour codes and the
     * {@code &} compatibility prefix, a code never contributes to the measured width, and the colour
     * of a run without a code is the one the vanilla label would use.
     */
    private static void testTextColours(Renderer renderer, Path root) throws Exception {
        // A container title keeps the vanilla label colour for its unformatted runs and centres on the
        // width of the text without its codes.
        BufferedImage plain = renderContainer(renderer, root, "inventory", Map.of("title", "Fixture plain", "rows", 3));
        BufferedImage styled = renderContainer(renderer, root, "inventory", Map.of("title", "\u00A7cFixture plain", "rows", 3));
        require(countExact(styled, 0xFF5555) > 4, "container title ignores the section colour code");
        require(exactBounds(styled, 0xFF5555)[0] == exactBounds(plain, LABEL_RGB)[0],
                "container title does not centre on the width without its colour code");

        // §r resets to the colour the caller passed in, exactly like an unstyled vanilla component.
        BufferedImage reset = renderContainer(renderer, root, "inventory",
                Map.of("title", "\u00A7cRed\u00A7r plain", "rows", 3));
        int[] resetRed = exactBounds(reset, 0xFF5555);
        int[] resetPlain = boundsOf(reset, LABEL_RGB);
        require(resetRed[2] < resetPlain[0], "§r does not reset the container title colour: "
                + java.util.Arrays.toString(resetRed) + " " + java.util.Arrays.toString(resetPlain));

        // A prefix that is not followed by a valid code character stays literal.
        BufferedImage literal = renderContainer(renderer, root, "inventory", Map.of("title", "A& B", "rows", 3));
        require(countExact(literal, 0xFF5555) == 0 && countExact(literal, 0x55FF55) == 0,
                "a literal ampersand was parsed as a colour code");

        // The inventory title uses the same pipeline at its vanilla 97 px column.
        Map<String, Object> inventory = new LinkedHashMap<>();
        inventory.put("operation", "player_inventory");
        inventory.put("resourceRoot", root.toString());
        inventory.put("playerInventory", Map.of(
                "skin", Map.of("texture", PlayerSkinFixture.png64()),
                "title", "&bBag"));
        require(countExact(renderer.render(inventory), 0x55FFFF) > 4, "inventory title ignores the ampersand colour code");

        // Tab list rows and the header/footer pair are styled as well, and a code does not widen the
        // row: the panel is laid out from the width of the text without its codes.
        Map<String, Object> player = Map.of("name", "Steve", "texture", PlayerSkinFixture.png64(), "ping", 40);
        Map<String, Object> styledPlayer = Map.of("name", "&cSteve", "texture", PlayerSkinFixture.png64(), "ping", 40);
        BufferedImage list = renderPlayerList(renderer, root, "&aHeader", "&bFooter", List.of(player));
        BufferedImage coded = renderPlayerList(renderer, root, "&aHeader", "&bFooter", List.of(styledPlayer));
        require(countExact(list, 0x55FF55) > 4, "playerlist header ignores the ampersand colour code");
        require(countExact(list, 0x55FFFF) > 4, "playerlist footer ignores the ampersand colour code");
        require(countExact(coded, 0xFF5555) > 4, "playerlist entry ignores the ampersand colour code");
        require(coded.getWidth() == list.getWidth(), "playerlist row width counts the colour codes: "
                + coded.getWidth() + " != " + list.getWidth());
        // Two header lines of the same text but different code lengths are centred on the same column.
        BufferedImage lines = renderPlayerList(renderer, root, "&aAB\n\u00A7cCD", "&bEF", List.of(player));
        require(exactBounds(lines, 0x55FF55)[0] == exactBounds(lines, 0xFF5555)[0],
                "playerlist header lines are not centred on the width without their codes");

        // The advancement toast and the map labels run through the same styling.
        Map<String, Object> achievement = new LinkedHashMap<>();
        achievement.put("operation", "achievement");
        achievement.put("resourceRoot", root.toString());
        achievement.put("achievement", Map.of(
                "title", "&cFixture", "description", "&9Done", "frame", "task", "completed", true,
                "icon", Map.of("id", "minecraft:diamond")));
        BufferedImage toast = renderer.render(achievement);
        require(countExact(toast, 0xFF5555) > 4, "achievement title ignores the ampersand colour code");
        require(countExact(toast, 0x5555FF) > 4, "achievement description ignores the ampersand colour code");

        // The tooltip pipeline accepts the compatibility prefix as well.
        Map<String, Object> tooltip = new LinkedHashMap<>();
        tooltip.put("operation", "tooltip");
        tooltip.put("resourceRoot", root.toString());
        tooltip.put("tooltip", Map.of("title", "&cCoded",
                "item", Map.of("id", "minecraft:diamond_sword", "lore", List.of("&aGreen line"))));
        BufferedImage tooltipImage = renderer.render(tooltip);
        require(countExact(tooltipImage, 0xFF5555) > 4, "tooltip ignores the ampersand colour code");
        require(countExact(tooltipImage, 0x55FF55) > 4, "tooltip ignores the ampersand colour code on its lore");

        Map<String, Object> map = new LinkedHashMap<>();
        map.put("operation", "map");
        map.put("resourceRoot", root.toString());
        map.put("map", Map.of("dimension", "&aOverworld",
                "markers", List.of(Map.of("x", 32, "z", 32, "rotation", 0, "type", "player", "name", "&eHome"))));
        BufferedImage mapImage = renderer.render(map);
        require(countExact(mapImage, 0x55FF55) > 4, "map dimension label ignores the ampersand colour code");
        require(countExact(mapImage, 0xFFFF55) > 4, "map marker name ignores the ampersand colour code");
    }

    /**
     * The arbitrary colour form of the legacy format, {@code §x§R§R§G§G§B§B}: the six prefix plus
     * hexadecimal digit pairs are one colour code, both prefixes and either digit case are accepted,
     * the code does not count towards the measured width and the style switches of the run it sits in
     * survive it. An incomplete sequence is not consumed at all.
     */
    private static void testHexColours(Renderer renderer, Path root) throws Exception {
        BufferedImage upper = titleTooltip(renderer, root, hexColour(0xA7, "6699FF", "Hex"));
        require(countExact(upper, 0x6699FF) > 4, "tooltip ignores the section hex colour code");

        // The compatibility prefix, lower case digits and a sequence whose six prefixes differ all
        // address the same colour.
        BufferedImage ampersand = titleTooltip(renderer, root, "&x&6&6&9&9&f&fHex");
        require(countExact(ampersand, 0x6699FF) > 4, "the ampersand hex form draws no pixels of its colour");
        BufferedImage mixed = titleTooltip(renderer, root,
                "&x" + section('6') + "&6" + section('9') + "&9" + section('F') + "&fHex");
        require(countExact(mixed, 0x6699FF) > 4, "mixing the two prefixes inside one hex code is rejected");

        // The code is not part of the measured width, so the box and the ink stay where the plain
        // title puts them.
        BufferedImage plain = titleTooltip(renderer, root, "Hex");
        require(upper.getWidth() == plain.getWidth(),
                "the hex code counts towards the tooltip width: " + upper.getWidth() + " != " + plain.getWidth());
        require(exactBounds(upper, 0x6699FF)[0] == exactBounds(plain, 0xFFFFFF)[0], "the hex code shifts the title");

        // A hex code only changes the colour: a §l in front of it still reaches the run it introduces,
        // so the title advances one extra pixel per glyph and its ink is thicker.
        String hexTitle = hexColour(0xA7, "FF33AA", "Bold");
        BufferedImage plainHex = renderContainer(renderer, root, "inventory", Map.of("title", hexTitle, "rows", 3));
        BufferedImage boldHex = renderContainer(renderer, root, "inventory",
                Map.of("title", section('l') + hexTitle, "rows", 3));
        int[] plainInk = exactBounds(plainHex, 0xFF33AA);
        int[] boldInk = exactBounds(boldHex, 0xFF33AA);
        require(boldInk[0] == plainInk[0], "the hex code shifts the bold title");
        require(boldInk[2] > plainInk[2], "a hex colour drops the bold advance of its run");
        require(countExact(boldHex, 0xFF33AA) > countExact(plainHex, 0xFF33AA), "a hex colour drops the bold ink");

        // A sequence that stops half way is left in the text: it is never read as 0x6699FF, and the
        // codes inside it keep applying one at a time.
        BufferedImage incomplete = titleTooltip(renderer, root, section('x') + section('6') + "Hex");
        require(countExact(incomplete, 0x6699FF) == 0, "an incomplete hex sequence was read as a colour");
        require(countExact(incomplete, 0xFFAA00) > 4, "the complete code inside an incomplete sequence was dropped");
        require(hasPixels(incomplete), "an incomplete hex sequence broke the tooltip");
    }

    /**
     * A tooltip line keeps the spaces between its words: a space is a glyph that advances four
     * pixels, not just a break opportunity, and only the space a line was actually broken at is
     * dropped.
     */
    private static void testWrappedSpaces(Renderer renderer, Path root) throws Exception {
        // The space advances the glyphs behind it, so the ink ends four pixels further right and the
        // box grows with it.
        BufferedImage spaced = titleTooltip(renderer, root, section('7') + "Label: 1");
        BufferedImage tight = titleTooltip(renderer, root, section('7') + "Label:1");
        int[] spacedInk = exactBounds(spaced, 0xAAAAAA);
        int[] tightInk = exactBounds(tight, 0xAAAAAA);
        require(spacedInk[0] == tightInk[0], "a space moves the start of the line");
        require(spacedInk[2] - tightInk[2] == 4,
                "the space of a tooltip line is dropped: " + spacedInk[2] + " is not four pixels past " + tightInk[2]);
        require(spaced.getWidth() - tight.getWidth() == 4, "the tooltip box does not count the space of its line");

        // Consecutive spaces are all kept.
        BufferedImage doubleSpaced = titleTooltip(renderer, root, "A  B");
        BufferedImage joined = titleTooltip(renderer, root, "AB");
        require(exactBounds(doubleSpaced, 0xFFFFFF)[2] - exactBounds(joined, 0xFFFFFF)[2] == 8,
                "consecutive spaces of a tooltip line are collapsed into one");

        // Wrapping is greedy over the cells including the spaces: the four two letter words fill two
        // lines at a 48 px wrap width while the same eight letters without spaces fit one.
        BufferedImage words = sizedTooltip(renderer, root, "AA BB CC DD", 72);
        BufferedImage glued = sizedTooltip(renderer, root, "AABBCCDD", 72);
        require(words.getHeight() == 2 * 10 + 24, "the spaces do not take part in the wrap: " + words.getHeight());
        require(glued.getHeight() == 1 * 10 + 24 - 2, "the space free title does not fit on one line");

        // The first line starts on the text column and keeps the gaps between its words, and the
        // continuation line does not start with the space the first one was broken at.
        int[] firstLine = inkBoundsIn(words, 0xFFFFFF, 12, 19);
        require(firstLine[0] == 12, "a wrapped line does not start on the text column: " + firstLine[0]);
        require(firstLine[2] >= 12 + 36, "the first line does not hold the words the wrap allows: " + firstLine[2]);
        require(widestInkGap(words, 0xFFFFFF, 12, 19) >= 3, "the word gaps of a wrapped line are lost");
        int[] secondLine = inkBoundsIn(words, 0xFFFFFF, 22, 29);
        require(secondLine[0] == 12, "a continuation line keeps the space the previous one broke at: " + secondLine[0]);
        require(countExact(words, 0xFFFFFF) > 4, "the wrapped lines drew no ink");
    }

    /**
     * Bold is the plain ink grown one pixel to the right, not the same glyph painted twice: every
     * glyph gains that one pixel, and the ink never shows a brightness step the plain glyph does not
     * have — which is exactly what compositing the same coverage twice used to add.
     */
    private static void testBoldGlyphs(Renderer renderer, Path root) throws Exception {
        // ASCII comes from the bitmap sheet: one ink colour, and the second glyph starts one pixel
        // later because the bold advance of the first one.
        BufferedImage asciiPlain = renderContainer(renderer, root, "inventory", Map.of("title", "AB", "rows", 3));
        BufferedImage asciiBold = renderContainer(renderer, root, "inventory",
                Map.of("title", section('l') + "AB", "rows", 3));
        int[] plainInk = exactBounds(asciiPlain, LABEL_RGB);
        int[] boldInk = exactBounds(asciiBold, LABEL_RGB);
        require(boldInk[0] == plainInk[0], "a bold glyph starts left of the plain one: " + boldInk[0]);
        require(boldInk[2] == plainInk[2] + 2, "the bold ink is not one pixel wider per glyph: "
                + boldInk[2] + " against " + plainInk[2]);
        require(coloursIn(asciiBold, 0, 20).equals(coloursIn(asciiPlain, 0, 20)),
                "bold adds a colour the plain bitmap glyph does not have");

        // Chinese has a five step coverage, so this is where a second pass would show: compositing the
        // same half covered pixel twice lands between two steps that cannot otherwise be reached.
        BufferedImage empty = renderContainer(renderer, root, "inventory", Map.of("rows", 3));
        BufferedImage cjkPlain = renderContainer(renderer, root, "inventory",
                Map.of("title", section('f') + "中", "rows", 3));
        BufferedImage cjkBold = renderContainer(renderer, root, "inventory",
                Map.of("title", section('f') + section('l') + "中", "rows", 3));
        java.util.Set<Integer> plainColours = coloursIn(cjkPlain, 0, 20);
        java.util.Set<Integer> boldColours = coloursIn(cjkBold, 0, 20);
        boldColours.removeAll(plainColours);
        require(boldColours.isEmpty(), "bold introduced " + boldColours.size()
                + " brightness steps the plain glyph never has");
        int plainCount = diffCountIn(empty, cjkPlain, 0, 0, 176, 20);
        int boldCount = diffCountIn(empty, cjkBold, 0, 0, 176, 20);
        require(boldCount > plainCount, "bold did not thicken the Chinese glyph: " + boldCount + " against " + plainCount);
        int[] plainBox = diffBounds(empty, cjkPlain);
        int[] boldBox = diffBounds(empty, cjkBold);
        require(boldBox[0] == plainBox[0], "the bold Chinese glyph starts left of the plain one: " + boldBox[0]);
        require(boldBox[2] == plainBox[2] + 1, "the bold Chinese glyph is not one pixel wider: "
                + boldBox[2] + " against " + plainBox[2]);

        // At twice the size the glyph is its texel grid, and the grown pixel must stay a whole one
        // instead of introducing a half covered column.
        BufferedImage scalePlain = renderContainerAt(renderer, root, section('f') + "中", 2);
        BufferedImage scaleBold = renderContainerAt(renderer, root, section('f') + section('l') + "中", 2);
        java.util.Set<Integer> scaleColours = coloursIn(scaleBold, 0, 40);
        scaleColours.removeAll(coloursIn(scalePlain, 0, 40));
        require(scaleColours.isEmpty(), "bold at twice the size introduced " + scaleColours.size()
                + " colours the plain glyph never has");
    }

    /**
     * A block icon is the 3D model seen through the vanilla gui pose and the orthographic icon camera:
     * the top face, two sides, each face mapped from its own texture quadrant and lit with the vanilla
     * block face shade of its own direction. That is what separates the three faces of a cube — the
     * entity lights this renderer used before made the top darker than the sides.
     */
    private static void testBlockIcon(Renderer renderer, Path root) throws Exception {
        // The fixture cube maps up, north and east to the red, green and blue quadrants of its sheet,
        // so each face colour is exactly the shade of its own direction.
        int up = 0xE03030;
        int north = shaded(0x30E030, 0.8);
        int east = shaded(0x3030E0, 0.6);
        BufferedImage icon = itemIcon(renderer, root, "minecraft:fixture_cube", 64);
        require(icon.getWidth() == 64 && icon.getHeight() == 64, "block icon dimensions: " + icon.getWidth());
        require(countExact(icon, up) > 200, "the up face is missing or not drawn at full brightness");
        require(countExact(icon, north) > 100, "the north face is missing or not shaded at 0.8");
        require(countExact(icon, east) > 100, "the east face is missing or not shaded at 0.6");
        require((icon.getRGB(0, 0) >>> 24) == 0, "the block icon fills its canvas, its corners must stay empty");

        // The three faces are where the isometric view puts them: the top above both sides, and the
        // east face left of the north one.
        double[] upCentre = centre(icon, up);
        double[] northCentre = centre(icon, north);
        double[] eastCentre = centre(icon, east);
        require(upCentre[1] < northCentre[1] && upCentre[1] < eastCentre[1],
                "the up face is not above the two sides: " + upCentre[1] + " " + northCentre[1] + " " + eastCentre[1]);
        require(eastCentre[0] < northCentre[0], "the two sides are not on opposite hands: "
                + eastCentre[0] + " " + northCentre[0]);

        // The icon is rasterised at its final size, so doubling it doubles the silhouette instead of
        // magnifying a 64 px picture.
        BufferedImage large = itemIcon(renderer, root, "minecraft:fixture_cube", 128);
        int[] smallBounds = exactBounds(icon, up);
        int[] largeBounds = exactBounds(large, up);
        for (int index = 0; index < 4; index++) {
            require(Math.abs(largeBounds[index] - smallBounds[index] * 2) <= 2,
                    "the 128 px icon is a magnified 64 px one: " + java.util.Arrays.toString(largeBounds)
                            + " against " + java.util.Arrays.toString(smallBounds));
        }
    }

    /**
     * One {@code §} code. Both the prefix and the code character are spelled from their code points:
     * a Java unicode escape for the section sign cannot survive every editor and diff tool.
     */
    private static String section(char code) {
        return (char) 0xA7 + String.valueOf(code);
    }

    /** The arbitrary colour form of the legacy format, {@code §x§R§R§G§G§B§B}, followed by {@code text}. */
    private static String hexColour(int prefix, String hex, String text) {
        StringBuilder builder = new StringBuilder().append((char) prefix).append('x');
        for (int index = 0; index < hex.length(); index++) {
            builder.append((char) prefix).append(hex.charAt(index));
        }
        return builder.append(text).toString();
    }

    /** Renders a tooltip whose only line is the given title. */
    private static BufferedImage titleTooltip(Renderer renderer, Path root, String title) throws Exception {
        return sizedTooltip(renderer, root, title, 0);
    }

    /** Renders a title only tooltip at a requested canvas width, which is what limits the wrap. */
    private static BufferedImage sizedTooltip(Renderer renderer, Path root, String title, int width) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "tooltip");
        request.put("resourceRoot", root.toString());
        request.put("tooltip", Map.of("title", title));
        if (width > 0) {
            request.put("width", width);
        }
        return renderer.render(request);
    }

    private static BufferedImage renderPlayerList(Renderer renderer, Path root, String header, String footer, List<Map<String, Object>> players) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "playerlist");
        request.put("resourceRoot", root.toString());
        request.put("playerList", Map.of("header", header, "footer", footer, "players", players));
        return renderer.render(request);
    }

    /** Browses a bounding box that may be absent, for assertions about a colour that must not occur. */
    private static int[] boundsOf(BufferedImage image, int rgb) {
        try {
            return exactBounds(image, rgb);
        } catch (AssertionError error) {
            return null;
        }
    }

    /**
     * The seven special model families the renderer transcribes from the client code. Every fixture
     * texture paints one rectangle per region of the vanilla cube layout with its own colour, and the
     * smoke test matches colours by their channel ratios because face shading scales all three
     * channels by the same factor: a colour that shows up proves which region was sampled.
     */
    private static void testSpecialItems(Renderer renderer, Path root) throws Exception {
        // The banner is the pole and the bar around a flag that carries the dye colour of the model
        // definition; the flag of banner_base must end up covered by the dyed layer on top of it.
        BufferedImage banner = renderItem(renderer, root, "minecraft:lime_banner", Map.of());
        requireContentSize(banner, "banner", 28, 58);
        require(countColor(banner, ColorKind.BANNER_FLAG) > 50, "banner flag is not the diffuse colour of its dye");
        require(countColor(banner, ColorKind.BANNER_POLE) > 20, "banner pole or bar did not sample entity/banner_base");
        require(countColor(banner, ColorKind.BANNER_HIDDEN) == 0, "the plain flag layer of banner_base is still visible");

        // The bed renders its head and foot layers with the tilt of BedRenderer.
        BufferedImage bed = renderItem(renderer, root, "minecraft:fixture_bed", Map.of());
        requireContentSize(bed, "bed", 55, 49);
        require(countColor(bed, ColorKind.BED_HEAD) > 100, "bed head layer missing");
        require(countColor(bed, ColorKind.BED_FOOT) > 100, "bed foot layer missing");

        // A skull is the head cube of its kind, turned to face the viewer.
        BufferedImage skull = renderItem(renderer, root, "minecraft:skeleton_skull", Map.of());
        requireContentSize(skull, "skeleton skull", 45, 49);
        require(countColor(skull, ColorKind.SKULL) > 100, "skeleton skull did not sample its head cube");
        // A player head adds the 0.25 inflated hat overlay, which hides the head cube underneath it.
        BufferedImage head = renderItem(renderer, root, "minecraft:player_head", Map.of());
        requireContentSize(head, "player head", 48, 52);
        require(countColor(head, ColorKind.HAT) > 100, "player head is missing the hat overlay");
        require(countColor(head, ColorKind.SKULL) == 0, "the head cube shows through the hat overlay");

        // The conduit icon is the inactive 6³ shell of the block entity renderer.
        BufferedImage conduit = renderItem(renderer, root, "minecraft:conduit", Map.of());
        requireContentSize(conduit, "conduit", 35, 37);
        require(countColor(conduit, ColorKind.CONDUIT) > 50, "conduit did not sample the shell texture");

        // The shield plate is drawn with the patternless sprite when the item carries no pattern.
        BufferedImage shield = renderItem(renderer, root, "minecraft:shield", Map.of());
        requireContentSize(shield, "shield", 35, 61);
        require(countColor(shield, ColorKind.SHIELD_PLATE) > 100, "shield plate missing");

        // The pot is the neck, the lid and the four sides of DecoratedPotRenderer.
        BufferedImage pot = renderItem(renderer, root, "minecraft:decorated_pot", Map.of());
        requireContentSize(pot, "decorated pot", 48, 59);
        require(countColor(pot, ColorKind.POT_NECK) > 20, "pot neck missing");
        require(countColor(pot, ColorKind.POT_LID) > 20, "pot lid missing");
        require(countColor(pot, ColorKind.POT_SIDE) > 100, "pot side plane missing");

        // The statue is the standing pose of the copper golem model.
        BufferedImage golem = renderItem(renderer, root, "minecraft:copper_golem_statue", Map.of());
        requireContentSize(golem, "copper golem statue", 29, 41);
        require(countColor(golem, ColorKind.GOLEM_HEAD) > 50, "copper golem head missing");
        require(countColor(golem, ColorKind.GOLEM_BODY) > 50, "copper golem body missing");

        // The chest family keeps the flat sprite of its entity texture.
        require(countExact(renderItem(renderer, root, "minecraft:chest", Map.of()), 0x204020) > 500,
                "chest family is not the flat sprite of its entity texture");

        // A shulker box is the closed lid and base cube of ShulkerBoxModel projected by the gui pose of
        // its model, not the flat entity sprite: three shaded faces of the box silhouette show, and the
        // texture parameter of the model definition picks the dye colour.
        BufferedImage shulker = renderItem(renderer, root, "minecraft:shulker_box", Map.of());
        requireContentSize(shulker, "shulker box", 56, 62);
        require(countColor(shulker, ColorKind.SHULKER_BOX) > 500, "shulker box did not sample entity/shulker/shulker");
        require(colourCount(shulker) >= 3, "shulker box is not a shaded 3D box");
        BufferedImage redShulker = renderItem(renderer, root, "minecraft:red_shulker_box", Map.of());
        requireContentSize(redShulker, "red shulker box", 56, 62);
        require(countColor(redShulker, ColorKind.SHULKER_RED) > 500,
                "red shulker box did not sample entity/shulker/shulker_red");
        require(countColor(redShulker, ColorKind.SHULKER_BOX) == 0, "red shulker box kept the undyed texture");
    }

    /**
     * The component driven layers of the special families: banner and shield patterns, the pot sherds
     * and the inline skin of a player head.
     */
    private static void testSpecialComponents(Renderer renderer, Path root) throws Exception {
        Map<String, Object> patterns = Map.of("banner_patterns", List.of(
                Map.of("pattern", "minecraft:fixture_pattern", "color", "purple")));
        BufferedImage patterned = renderItem(renderer, root, "minecraft:lime_banner", patterns);
        require(countColor(patterned, ColorKind.BANNER_PATTERN) > 4, "banner pattern layer is missing");
        require(countColor(patterned, ColorKind.BANNER_FLAG) > 50, "banner pattern layer replaced the flag");

        // A base_color component overrides the dye the model definition names.
        BufferedImage recoloured = renderItem(renderer, root, "minecraft:white_banner", Map.of("base_color", "purple"));
        require(countColor(recoloured, ColorKind.BANNER_PATTERN) > 50, "base_color does not override the model colour");

        BufferedImage shield = renderItem(renderer, root, "minecraft:fixture_shield", patterns);
        require(countColor(shield, ColorKind.SHIELD_PATTERN) > 4, "shield pattern layer is missing");

        // The pot decorations select one pottery pattern sprite per side, in the vanilla order.
        BufferedImage pot = renderItem(renderer, root, "minecraft:decorated_pot", Map.of("pot_decorations", List.of(
                "minecraft:fixture_pottery_sherd", "minecraft:fixture_pottery_sherd",
                "minecraft:fixture_pottery_sherd", "minecraft:fixture_pottery_sherd")));
        require(countColor(pot, ColorKind.POT_PATTERN) > 100, "pottery pattern sprite is not used");
        require(countColor(pot, ColorKind.POT_SIDE) == 0, "a plain pot side survived the pottery pattern");

        // A player head reads the inline skin of its profile property; its hat overlay then carries the
        // colour of that skin instead of the one of the default skin.
        String document = "{\"textures\":{\"SKIN\":{\"url\":\"" + inlineSkin() + "\"}}}";
        String property = java.util.Base64.getEncoder().encodeToString(document.getBytes(java.nio.charset.StandardCharsets.UTF_8));
        BufferedImage profile = renderItem(renderer, root, "minecraft:player_head", Map.of("profile", Map.of(
                "properties", List.of(Map.of("name", "textures", "value", property)))));
        require(countColor(profile, ColorKind.PROFILE_HAT) > 100, "the inline profile skin is not used");
        require(countColor(profile, ColorKind.HAT) == 0, "the default skin survived the inline profile");
        // A profile that names a remote skin returns to the default skin of the client.
        BufferedImage remote = renderItem(renderer, root, "minecraft:player_head", Map.of("profile", Map.of(
                "properties", List.of(Map.of("name", "textures",
                        "value", java.util.Base64.getEncoder().encodeToString(
                                "{\"textures\":{\"SKIN\":{\"url\":\"http://example.invalid/skin\"}}}"
                                        .getBytes(java.nio.charset.StandardCharsets.UTF_8)))))));
        require(countColor(remote, ColorKind.HAT) > 100, "a remote profile does not fall back to the default skin");
    }

    /**
     * A 64x64 skin whose head and hat rectangles carry their own colours: the humanoid head samples
     * {@code 0..32 x 0..16} for the cube and {@code 32..64 x 0..16} for the hat overlay, so the second
     * rectangle is what a head icon shows.
     */
    private static String inlineSkin() throws Exception {
        BufferedImage image = new BufferedImage(64, 64, BufferedImage.TYPE_INT_ARGB);
        for (int y = 0; y < 16; y++) {
            for (int x = 0; x < 64; x++) {
                image.setRGB(x, y, 0xFF000000 | (x < 32 ? 0x3030B0 : 0xFF9F40));
            }
        }
        java.io.ByteArrayOutputStream output = new java.io.ByteArrayOutputStream();
        ImageIO.write(image, "png", output);
        return "data:image/png;base64," + java.util.Base64.getEncoder().encodeToString(output.toByteArray());
    }

    /** A held special item is a 3D model of its own, so it must sit in front of the torso like a sword. */
    private static void testSpecialHeldItems(Renderer renderer, Path root) throws Exception {
        BufferedImage none = renderPlayer(renderer, root, "classic", false, List.of());
        for (String id : List.of("minecraft:lime_banner", "minecraft:conduit", "minecraft:decorated_pot",
                "minecraft:copper_golem_statue", "minecraft:trident", "minecraft:skeleton_skull")) {
            BufferedImage held = renderEquipment(renderer, root, Map.of("mainHand", Map.of("id", id)));
            int[] bounds = contentBounds(held);
            require(bounds[0] > 0 && bounds[2] < held.getWidth() - 1
                            && bounds[1] > 0 && bounds[3] < held.getHeight() - 1,
                    "held " + id + " is clipped by the canvas: " + java.util.Arrays.toString(bounds));
            require(diffCountIn(none, held, 0, 0, held.getWidth(), held.getHeight()) > 40,
                    "held " + id + " is not visible");
        }
        // The banner is the only family whose fixture texture can prove that the item hangs in front of
        // the body rather than behind its back: its pole colour is unique to that item.
        BufferedImage banner = renderEquipment(renderer, root, Map.of("mainHand", Map.of("id", "minecraft:lime_banner")));
        int overBody = overModelPixels(none, banner, bannerPole(banner), -1);
        require(countColor(banner, ColorKind.BANNER_FLAG) > 20, "held banner is missing its flag");
        require(overBody > 20, "held banner is not drawn in front of the torso");
    }

    /** The exact colour of the fixture banner pole, read back from the icon to keep the assertion shade free. */
    private static int bannerPole(BufferedImage image) {
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) > 200 && matches(pixel, ColorKind.BANNER_POLE)) {
                    return pixel & 0xFFFFFF;
                }
            }
        }
        throw new AssertionError("no banner pole pixels found");
    }

    /**
     * The item list of the consumer that used to be rejected with a 422: every one of the 51 items
     * whose model is a special model outside the chest family must render a non-empty icon.
     */
    private static void testSpecialItemList(Renderer renderer, Path root) throws Exception {
        String[] items = {
                "black_banner", "black_bed", "blue_banner", "blue_bed", "brown_banner", "brown_bed", "conduit",
                "copper_golem_statue", "creeper_head", "cyan_banner", "cyan_bed", "decorated_pot", "dragon_head",
                "exposed_copper_golem_statue", "gray_banner", "gray_bed", "green_banner", "green_bed",
                "light_blue_banner", "light_blue_bed", "light_gray_banner", "light_gray_bed", "lime_banner",
                "lime_bed", "magenta_banner", "magenta_bed", "orange_banner", "orange_bed",
                "oxidized_copper_golem_statue", "piglin_head", "pink_banner", "pink_bed", "player_head",
                "purple_banner", "purple_bed", "red_banner", "red_bed", "red_shulker_box", "shield",
                "shulker_box", "skeleton_skull", "trident",
                "waxed_copper_golem_statue", "waxed_exposed_copper_golem_statue",
                "waxed_oxidized_copper_golem_statue", "waxed_weathered_copper_golem_statue",
                "weathered_copper_golem_statue", "white_banner", "white_bed", "wither_skeleton_skull",
                "yellow_banner", "yellow_bed", "zombie_head"};
        require(items.length == 53, "the special item list changed size: " + items.length);
        for (String item : items) {
            BufferedImage icon = renderItem(renderer, root, "minecraft:" + item, Map.of());
            require(icon.getWidth() == 64 && icon.getHeight() == 64, item + " icon dimensions");
            require(countAlpha(icon, 40) > 100, item + " rendered an empty icon");
        }
    }

    /**
     * A broken resource must never take the whole picture down: an unknown item, an absent texture and a
     * special family this service has not ported all fall back to the vanilla missing model.
     */
    /**
     * Chinese text runs through the same font pipeline as every other label: the client's {@code
     * default} provider chain, whose {@code unihex} provider draws the 16x16 Unifont bitmap at half
     * scale. That gives a full width glyph an advance of nine font pixels, two by two supersampled
     * ink, and the same seven pixel ascent a Latin glyph uses — which is what every surface below
     * (tooltip, container title, tab list, map label, toast) is measured and drawn with.
     */
    private static void testChineseFont(Renderer renderer, Path root) throws Exception {
        MinecraftFont font = MinecraftFont.load(Renderer.ResourceStack.from(Map.of(), root.toString()));

        // Advances are the vanilla ones: five plus one for a capital, four for the space provider and
        // half of the sixteen texel bitmap plus one for a full width character.
        require(font.advance('A') == 6, "ASCII advance changed: " + font.advance('A'));
        require(font.advance(' ') == 4, "space advance changed: " + font.advance(' '));
        require(font.advance('\u4E2D') == 9, "Chinese advance is not nine pixels: " + font.advance('\u4E2D'));
        require(font.advance('\uFF0C') == 9, "full width comma is not nine pixels: " + font.advance('\uFF0C'));
        require(font.width("\u4E2D\u6587\u6D4B\u8BD5") == 36, "four Chinese characters are not 36 px wide");
        require(font.width("\u4E2D\u6587ABC") == 9 + 9 + 6 * 3, "mixed Chinese and Latin width is wrong");

        // Chinese ink is the Unifont bitmap, not a vector approximation: eight by eight pixels from
        // the seven pixel ascent with the box and the centre stroke of the in game glyph.
        MinecraftFont.Glyph zhong = font.glyph('\u4E2D');
        require(zhong.width() == 8 && zhong.height() == 8 && zhong.ascent() == 7,
                "Chinese glyph box is not the eight pixel Unifont cell");
        String[] expected = {
                "...#....", "...#....", ".######.", ".#.#..#.",
                ".#.#..#.", ".######.", "...#....", "...#...."};
        for (int row = 0; row < expected.length; row++) {
            for (int column = 0; column < 8; column++) {
                boolean wanted = expected[row].charAt(column) == '#';
                require(zhong.inked(column, row) == wanted,
                        "Unifont pixel " + column + "," + row + " of U+4E2D is wrong");
            }
        }
        // Half scale means most CJK pixels cover two or three of their four texels, so the glyph keeps
        // its greys instead of collapsing into solid ink; only a bitmap sheet paints whole pixels.
        int[] stroke = {3, 2, 3, 2, 2, 2};
        for (int column = 0; column < stroke.length; column++) {
            require(zhong.coverage(column + 1, 2) == stroke[column],
                    "coverage of the middle stroke of U+4E2D is not the texel share");
        }
        require(zhong.coverage(0, 0) == 0, "the empty corner of U+4E2D carries ink");

        // Drawn at twice the size one texel lands on one pixel, so the glyph is the sixteen texel
        // Unifont bitmap itself: no intermediate greys, and exactly one opaque pixel per lit texel.
        // Magnifying the eight pixel average instead is what made Chinese at twice the size blurred.
        BufferedImage doubled = new BufferedImage(32, 32, BufferedImage.TYPE_INT_ARGB);
        Graphics2D doubledGraphics = doubled.createGraphics();
        zhong.draw(doubledGraphics, 0, 0, 2, 2, 0xFFFFFF, false, false);
        doubledGraphics.dispose();
        int opaque = 0;
        int grey = 0;
        for (int row = 0; row < 32; row++) {
            for (int column = 0; column < 32; column++) {
                int alpha = doubled.getRGB(column, row) >>> 24;
                if (alpha == 255) {
                    opaque++;
                } else if (alpha != 0) {
                    grey++;
                }
            }
        }
        require(grey == 0, "the doubled Chinese glyph keeps " + grey + " partly transparent pixels");
        int quarters = 0;
        for (int row = 0; row < zhong.height(); row++) {
            for (int column = 0; column < zhong.width(); column++) {
                quarters += zhong.coverage(column, row);
            }
        }
        require(opaque == quarters,
                "the doubled glyph carries " + opaque + " pixels of ink but " + quarters + " texels");

        // Latin still comes from ascii.png and an accented glyph keeps the taller accented cell, so
        // the sheet a code point belongs to is decided by the provider chain and not by its range.
        MinecraftFont.Glyph capital = font.glyph('A');
        require(capital.width() == 5 && capital.height() == 8 && capital.ascent() == 7, "ASCII glyph geometry changed");
        MinecraftFont.Glyph accented = font.glyph('\u00E9');
        require(accented.ascent() == 10 && accented.height() == 12, "accented sheet geometry changed");

        // The tooltip derives its box from the Chinese advance and paints the glyphs.
        Map<String, Object> tooltip = new LinkedHashMap<>();
        tooltip.put("operation", "tooltip");
        tooltip.put("resourceRoot", root.toString());
        tooltip.put("tooltip", Map.of("title", "\u4E2D\u6587\u6D4B\u8BD5"));
        BufferedImage tooltipImage = renderer.render(tooltip);
        require(tooltipImage.getWidth() == 36 + 24, "tooltip width ignores the Chinese advance: " + tooltipImage.getWidth());
        require(countBright(tooltipImage, 0xFFFFFF) > 40, "Chinese tooltip title drew no ink");

        // A container title sits on the vanilla title column and spans four full width cells.
        Map<String, Object> container = new LinkedHashMap<>();
        container.put("operation", "inventory");
        container.put("resourceRoot", root.toString());
        container.put("container", Map.of("title", "\u00A7c\u4E2D\u6587\u6D4B\u8BD5", "rows", 3, "slots", List.of()));
        BufferedImage containerImage = renderer.render(container);
        int[] label = tintedBounds(containerImage, 0xFF5555, 40);
        // Four full width cells advance 36 px from the vanilla title column at x = 8. The inked box
        // of U+4E2D starts one pixel inside its cell and the ink of the last glyph stays inside the
        // fourth cell, so the title ends before x = 8 + 36.
        require(label[0] == 9, "container title fell outside its font cell: " + label[0]);
        require(label[2] >= 40 && label[2] <= 43, "container title ink does not span four cells: " + label[2]);

        // Tab list rows, header and footer, map labels and the advancement toast accept Chinese with
        // the same metrics as the tooltip.
        Map<String, Object> player = Map.of("name", "\u73A9\u5BB6", "texture", PlayerSkinFixture.png64(), "ping", 40);
        BufferedImage list = renderPlayerList(renderer, root, "\u4E2D\u6587\u5934", "\u4E16\u754C", List.of(player));
        require(countBright(list, 0xFFFFFF) > 40, "playerlist header drew no Chinese ink");

        Map<String, Object> map = new LinkedHashMap<>();
        map.put("operation", "map");
        map.put("resourceRoot", root.toString());
        map.put("map", Map.of("dimension", "\u4E3B\u4E16\u754C",
                "markers", List.of(Map.of("x", 32, "z", 32, "rotation", 0, "type", "player", "name", "\u57CE\u5E02"))));
        BufferedImage mapImage = renderer.render(map);
        require(countBright(mapImage, 0xFFFFFF) > 40, "map labels drew no Chinese ink");

        Map<String, Object> achievement = new LinkedHashMap<>();
        achievement.put("operation", "achievement");
        achievement.put("resourceRoot", root.toString());
        achievement.put("achievement", Map.of(
                "title", "\u4EFB\u52A1", "description", "\u5B8C\u6210\u4E86", "frame", "task", "completed", true,
                "icon", Map.of("id", "minecraft:diamond")));
        BufferedImage toast = renderer.render(achievement);
        require(countTinted(toast, 0xFFFF55, 40) > 20, "achievement title drew no Chinese ink");
    }

    /**
     * A resource tree whose font definition is missing or whose whole provider chain fails to load
     * has to fail with a message that names the repair. Drawing the missing glyph instead answers
     * every request with a picture of empty boxes, which reads like a renderer bug rather than the
     * missing {@code assets/minecraft/font} overlay it is.
     */
    private static void testMissingFontDefinition(Path root) throws Exception {
        Path empty = Files.createTempDirectory("minecraft-font-missing");
        try {
            requireFontFailure(empty, "font/default.json", "a tree without font/default.json");
            Path definition = empty.resolve("assets/minecraft/font/default.json");
            Files.createDirectories(definition.getParent());
            Files.copy(root.resolve("assets/minecraft/font/default.json"), definition);
            requireFontFailure(empty, "no font provider", "a tree whose includes are all missing");
        } finally {
            try (var files = Files.walk(empty)) {
                files.sorted(java.util.Comparator.reverseOrder()).forEach(path -> {
                    try {
                        Files.deleteIfExists(path);
                    } catch (java.io.IOException ignored) {
                        // Best effort cleanup of a temporary tree.
                    }
                });
            }
        }
    }

    private static void requireFontFailure(Path root, String expected, String what) throws Exception {
        try {
            MinecraftFont.load(Renderer.ResourceStack.from(Map.of(), root.toString()));
        } catch (java.io.IOException error) {
            String message = String.valueOf(error.getMessage());
            require(message.contains(expected), what + " failed with an unexpected message: " + message);
            require(message.contains("prepare"), what + " does not name the repair: " + message);
            return;
        }
        require(false, what + " must not load a font");
    }

    private static void testMissingResources(Renderer renderer, Path root) throws Exception {
        BufferedImage missingModel = renderItem(renderer, root, "minecraft:not_an_item", Map.of());
        requireMissingModel(missingModel, "an unknown item");

        // The vanilla missing texture is the generated checkerboard, and it is reported on stderr.
        BufferedImage missingTexture = renderItem(renderer, root, "minecraft:diamond_sword",
                Map.of("minecraft:item_model", "minecraft:item/absent_model"));
        requireMissingModel(missingTexture, "an absent model");

        // An unimplemented special family degrades the same way instead of failing the request.
        BufferedImage unsupported = renderItem(renderer, root, "minecraft:fixture_unsupported_special", Map.of());
        requireMissingModel(unsupported, "an unported special family");

        // The same fallback keeps a container render alive when one slot is broken.
        BufferedImage container = renderContainer(renderer, root, "inventory", Map.of("rows", 3,
                "slots", List.of(Map.of("slot", 0, "item", Map.of("id", "minecraft:not_an_item")))));
        requireMissingModel(container, "a broken slot item");

        // A structural request error stays a hard failure.
        Map<String, Object> empty = new LinkedHashMap<>();
        empty.put("operation", "item");
        empty.put("resourceRoot", root.toString());
        empty.put("item", Map.of());
        requireFails(renderer, empty, "item.id is required");
    }

    /**
     * The size of the model one family produced, which catches a misread box or scale: the fixture
     * paints one flat colour per texture rectangle, so the silhouette is the geometry itself.
     */
    private static void requireContentSize(BufferedImage image, String what, int width, int height) {
        int[] bounds = contentBounds(image);
        int actualWidth = bounds[2] - bounds[0] + 1;
        int actualHeight = bounds[3] - bounds[1] + 1;
        require(Math.abs(actualWidth - width) <= 3 && Math.abs(actualHeight - height) <= 3,
                what + " is not the vanilla size: " + actualWidth + "x" + actualHeight
                        + " instead of " + width + "x" + height);
    }

    /**
     * The vanilla missing model is a 16³ cube of the generated missing texture: its two magenta and its
     * two black quadrants are shaded per face, so the check matches the magenta by its channel ratio.
     */
    private static void requireMissingModel(BufferedImage image, String what) {
        require(countColor(image, ColorKind.MISSING_MAGENTA) > 40 && countColor(image, ColorKind.DECORATION_BLACK) > 40,
                what + " does not fall back to the missing model");
    }

    private static BufferedImage renderItem(Renderer renderer, Path root, String id, Map<String, Object> components) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "item");
        request.put("resourceRoot", root.toString());
        request.put("width", 64);
        Map<String, Object> item = new LinkedHashMap<>();
        item.put("id", id);
        if (!components.isEmpty()) {
            item.put("components", components);
        }
        request.put("item", item);
        return renderer.render(request);
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

    /** Bounding box of every pixel that is visible at all, for the clipping assertions. */
    private static int[] contentBounds(BufferedImage image) {
        int[] bounds = null;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if ((image.getRGB(x, y) >>> 24) < 40) {
                    continue;
                }
                bounds = extend(bounds, x, y);
            }
        }
        require(bounds != null, "image is fully transparent");
        return bounds;
    }

    private static int[] extend(int[] bounds, int x, int y) {        if (bounds == null) {
            return new int[]{x, y, x, y};
        }
        bounds[0] = Math.min(bounds[0], x);
        bounds[1] = Math.min(bounds[1], y);
        bounds[2] = Math.max(bounds[2], x);
        bounds[3] = Math.max(bounds[3], y);
        return bounds;
    }

    /** Bounding box of every pixel that differs between two renders of the same size. */
    private static int[] diffBounds(BufferedImage before, BufferedImage after) {
        require(before.getWidth() == after.getWidth() && before.getHeight() == after.getHeight(), "diffed images differ in size");
        int[] bounds = null;
        for (int y = 0; y < before.getHeight(); y++) {
            for (int x = 0; x < before.getWidth(); x++) {
                if (before.getRGB(x, y) != after.getRGB(x, y)) {
                    bounds = extend(bounds, x, y);
                }
            }
        }
        require(bounds != null, "the two images are identical");
        return bounds;
    }

    /**
     * Counts the pixels of one exact colour in {@code after} that covered the model of
     * {@code before} — opaque and, when a background colour is given, different from it — which is
     * how a held item drawn in front of the body is told apart from one hidden behind it.
     */
    private static int overModelPixels(BufferedImage before, BufferedImage after, int rgb, int background) {
        require(before.getWidth() == after.getWidth() && before.getHeight() == after.getHeight(), "diffed images differ in size");
        int count = 0;
        for (int y = 0; y < after.getHeight(); y++) {
            for (int x = 0; x < after.getWidth(); x++) {
                if ((after.getRGB(x, y) & 0xFFFFFF) != rgb) {
                    continue;
                }
                int pixel = before.getRGB(x, y);
                if ((pixel >>> 24) <= 200 || (background >= 0 && (pixel & 0xFFFFFF) == (background & 0xFFFFFF))) {
                    continue;
                }
                count++;
            }
        }
        return count;
    }

    /** Counts the pixels that differ between two renders inside one rectangle. */
    private static int diffCountIn(BufferedImage before, BufferedImage after, int x0, int y0, int x1, int y1) {
        int count = 0;
        for (int y = Math.max(0, y0); y < Math.min(before.getHeight(), y1); y++) {
            for (int x = Math.max(0, x0); x < Math.min(before.getWidth(), x1); x++) {
                if (before.getRGB(x, y) != after.getRGB(x, y)) {
                    count++;
                }
            }
        }
        return count;
    }

    private static boolean equalPixels(BufferedImage first, BufferedImage second) {
        if (first.getWidth() != second.getWidth() || first.getHeight() != second.getHeight()) {
            return false;
        }
        for (int y = 0; y < first.getHeight(); y++) {
            for (int x = 0; x < first.getWidth(); x++) {
                if (first.getRGB(x, y) != second.getRGB(x, y)) {
                    return false;
                }
            }
        }
        return true;
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

    /** The ink bounds of one exact colour inside a row band, for asserting a single tooltip line. */
    private static int[] inkBoundsIn(BufferedImage image, int rgb, int y0, int y1) {
        int[] bounds = null;
        for (int y = y0; y <= y1; y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) == 255 && (pixel & 0xFFFFFF) == rgb) {
                    bounds = extend(bounds, x, y);
                }
            }
        }
        require(bounds != null, "no pixels for colour " + Integer.toHexString(rgb) + " in rows " + y0 + ".." + y1);
        return bounds;
    }

    /** The widest run of ink free columns that sits between two inked columns of a row band. */
    private static int widestInkGap(BufferedImage image, int rgb, int y0, int y1) {
        boolean[] inked = new boolean[image.getWidth()];
        for (int y = y0; y <= y1; y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                inked[x] |= (pixel >>> 24) == 255 && (pixel & 0xFFFFFF) == rgb;
            }
        }
        int gap = 0;
        int empty = 0;
        boolean seen = false;
        for (boolean column : inked) {
            if (column) {
                if (seen) {
                    gap = Math.max(gap, empty);
                }
                seen = true;
                empty = 0;
            } else if (seen) {
                empty++;
            }
        }
        return gap;
    }

    /** Renders one container title at a render scale, which is the device pixel scale of the text. */
    private static BufferedImage renderContainerAt(Renderer renderer, Path root, String title, int scale) throws Exception {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("operation", "inventory");
        request.put("resourceRoot", root.toString());
        request.put("pixelScale", scale);
        request.put("container", Map.of("title", title, "rows", 3));
        return renderer.render(request);
    }

    /** The ARGB values that appear inside a row band of an image. */
    private static java.util.Set<Integer> coloursIn(BufferedImage image, int y0, int y1) {
        java.util.Set<Integer> colours = new java.util.HashSet<>();
        for (int y = y0; y <= y1; y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                colours.add(image.getRGB(x, y));
            }
        }
        return colours;
    }

    /**
     * Counts the pixels that carry an untinted ink colour over a darker surface. A {@code unihex} glyph
     * is drawn at half scale, so most Chinese pixels cover only part of their texels and are
     * composited with a matching alpha: an exact colour match would only ever see the bitmap sheets.
     * A pixel counts when every channel is at least a third of the ink's, which keeps the dark panels,
     * shadows and sprite borders the labels sit on out of the count.
     */
    private static int countBright(BufferedImage image, int rgb) {
        int red = rgb >> 16 & 0xFF;
        int green = rgb >> 8 & 0xFF;
        int blue = rgb & 0xFF;
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) != 255) {
                    continue;
                }
                if ((pixel >> 16 & 0xFF) * 3 >= red && (pixel >> 8 & 0xFF) * 3 >= green
                        && (pixel & 0xFF) * 3 >= blue) {
                    count++;
                }
            }
        }
        return count;
    }

    /**
     * Counts the pixels that keep the channel pattern of a tinted ink over a neutral surface. Blending
     * scales every channel difference by the coverage, so a translucent pixel of a styled Chinese
     * label still shows the ink's order — which is what this compares, with {@code margin} keeping the
     * grey panels and slot borders out of the count.
     */
    private static int countTinted(BufferedImage image, int rgb, int margin) {
        int count = 0;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if (tinted(image.getRGB(x, y), rgb, margin)) {
                    count++;
                }
            }
        }
        return count;
    }

    private static int[] tintedBounds(BufferedImage image, int rgb, int margin) {
        int[] bounds = null;
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                if (tinted(image.getRGB(x, y), rgb, margin)) {
                    bounds = extend(bounds, x, y);
                }
            }
        }
        require(bounds != null, "no tinted pixels for colour " + Integer.toHexString(rgb));
        return bounds;
    }

    private static boolean tinted(int pixel, int rgb, int margin) {
        if ((pixel >>> 24) != 255) {
            return false;
        }
        int[] ink = {rgb >> 16 & 0xFF, rgb >> 8 & 0xFF, rgb & 0xFF};
        int[] channel = {pixel >> 16 & 0xFF, pixel >> 8 & 0xFF, pixel & 0xFF};
        for (int first = 0; first < 3; first++) {
            for (int second = 0; second < 3; second++) {
                if (ink[first] - ink[second] > margin && channel[first] - channel[second] < margin) {
                    return false;
                }
            }
        }
        return true;
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
        MAP_GRASS, MAP_SAND, DECORATION_WHITE, DECORATION_BLACK, PING,
        BANNER_FLAG, BANNER_POLE, BANNER_HIDDEN, BANNER_PATTERN,
        BED_HEAD, BED_FOOT, SKULL, CONDUIT, SHIELD_PLATE, SHIELD_PATTERN,
        POT_NECK, POT_LID, POT_SIDE, POT_PATTERN, GOLEM_HEAD, GOLEM_BODY, PROFILE_HAT, MISSING_MAGENTA,
        SHULKER_BOX, SHULKER_RED
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
            // The special item fixture: every colour below is the flat colour of one texture rectangle
            // of the vanilla cube layout, chosen to be distinct from all the other kinds.
            case BANNER_FLAG -> new double[]{0.32, 1, 0.25};
            case BANNER_POLE -> new double[]{0.6, 0.6, 1};
            case BANNER_HIDDEN -> new double[]{0.27, 0.27, 1};
            case BANNER_PATTERN -> new double[]{0.65, 0.25, 1};
            case BED_HEAD -> new double[]{0.6, 1, 0.6};
            case BED_FOOT -> new double[]{0.6, 1, 1};
            case SKULL -> new double[]{0.43, 0.71, 1};
            case CONDUIT -> new double[]{0.33, 0.56, 1};
            case SHIELD_PLATE -> new double[]{0.43, 1, 0.43};
            case SHIELD_PATTERN -> new double[]{0.65, 0.25, 1};
            case POT_NECK -> new double[]{0.43, 1, 1};
            case POT_LID -> new double[]{0.27, 1, 0.45};
            case POT_SIDE -> new double[]{0.27, 1, 0.64};
            case POT_PATTERN -> new double[]{0.27, 1, 0.82};
            case GOLEM_HEAD -> new double[]{0.27, 1, 1};
            case GOLEM_BODY -> new double[]{1, 0.6, 1};
            case PROFILE_HAT -> new double[]{1, 0.62, 0.25};
            case SHULKER_BOX -> new double[]{0.6, 0.6, 1};
            case SHULKER_RED -> new double[]{1, 0.27, 0.25};
            // The magenta quadrant of the generated vanilla missing texture.
            case MISSING_MAGENTA -> new double[]{1, 0, 1};
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

    /** The number of distinct colours of the opaque pixels: a shaded box shows one per visible face. */
    private static int colourCount(BufferedImage image) {
        java.util.Set<Integer> colours = new java.util.HashSet<>();
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int pixel = image.getRGB(x, y);
                if ((pixel >>> 24) > 200) {
                    colours.add(pixel & 0xFFFFFF);
                }
            }
        }
        return colours.size();
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
