package com.illtamer.minecraft;

import java.awt.Color;
import java.awt.Graphics2D;
import java.awt.geom.Rectangle2D;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;

/**
 * The Minecraft font pipeline, ported from the client so that every label of this service uses the
 * same glyphs, advances and baselines as the game.
 *
 * <p>The chain is the one {@code assets/minecraft/font/default.json} declares: the {@code space}
 * advances, the bitmap sheets of {@code include/default} (ascii, accented, non latin european) and
 * finally {@code include/unifont}. That last include is what draws Chinese: a {@code unihex}
 * provider with the 16x16 Unifont bitmaps, not a vector outline. A provider is consulted in
 * declaration order and the first hit wins, exactly like the client, so a Chinese character can
 * never fall through to a Latin sheet.</p>
 *
 * <p>Metrics follow the client as well. Every provider sits on the seven pixel ascent the {@code
 * fwq} interface defines, bitmap glyphs are eight (or, for the accented sheet, twelve) pixels tall
 * starting at their own ascent, and a {@code unihex} glyph is sixteen texels drawn at half scale, so
 * it occupies eight pixels of the eight pixel cell with its top on the ascent line. Advances are the
 * per glyph values the client computes: the last inked column plus one for a bitmap glyph and half
 * the inked column count plus one for a {@code unihex} glyph.</p>
 *
 * <p>A glyph is stored as per pixel coverage rather than as a bitmask. Sheets paint full pixels, but
 * the half scale of the {@code unihex} provider leaves most CJK edges covering only one or two of
 * their four texels, and keeping those as greys is what stops Chinese labels from turning into blocks
 * of solid ink.</p>
 */
final class MinecraftFont {
    /** The line ascent every provider aligns to, {@code fwq.a} in the client. */
    static final int ASCENT = 7;
    /** The line height of the client font, used by the tooltip and chat layouts. */
    static final int LINE_HEIGHT = 9;

    private static final String DEFAULT_DEFINITION = "assets/minecraft/font/default.json";
    private static final int MAX_REFERENCE_DEPTH = 8;
    /** The advance and shape of a code point no provider covers, a visible five by seven box. */
    private static final Glyph MISSING = Glyph.box(6, 7);

    private final List<Provider> providers;
    private final Map<Integer, Glyph> cache = new HashMap<>();
    private final Set<Integer> unresolved = new HashSet<>();

    private MinecraftFont(List<Provider> providers) {
        this.providers = List.copyOf(providers);
    }

    /**
     * Loads the {@code default} font of the resource context.
     *
     * <p>A tree without a usable definition is refused instead of drawn: falling back to the missing
     * glyph would answer every request with a picture whose labels are empty boxes, which reads like
     * a rendering bug rather than the missing {@code assets/minecraft/font} overlay it is. The
     * message names the repair so the caller can act on it.</p>
     */
    static MinecraftFont load(Renderer.ResourceStack resources) throws IOException {
        byte[] data = resources.bytes(DEFAULT_DEFINITION);
        if (data == null) {
            throw new IOException("missing " + DEFAULT_DEFINITION
                    + " in the resource tree; prepare this Minecraft version again");
        }
        Set<String> options = Set.of();
        List<Provider> providers = readProviders(resources, DEFAULT_DEFINITION, options, 0);
        if (providers.isEmpty()) {
            throw new IOException("no font provider could be loaded from " + DEFAULT_DEFINITION
                    + "; the resource tree is incomplete, prepare this Minecraft version again");
        }
        return new MinecraftFont(providers);
    }

    /**
     * The glyph of one code point. Providers are asked in declaration order and the first answer
     * wins; a code point no provider covers answers the missing glyph so a broken resource stays
     * visible instead of silently dropping text.
     */
    Glyph glyph(int codePoint) {
        Glyph cached = cache.get(codePoint);
        if (cached != null) {
            return cached;
        }
        for (Provider provider : providers) {
            Glyph glyph = provider.glyph(codePoint);
            if (glyph != null) {
                cache.put(codePoint, glyph);
                return glyph;
            }
        }
        if (unresolved.add(codePoint)) {
            System.err.printf("no font provider covers U+%04X, drawing the missing glyph%n", codePoint);
        }
        cache.put(codePoint, MISSING);
        return MISSING;
    }

    /** The advance of one code point in font pixels, the unit every vanilla label coordinate uses. */
    int advance(int codePoint) {
        return glyph(codePoint).advance();
    }

    /** The advance of plain text: one provider lookup per code point, summed. */
    int width(String text) {
        int width = 0;
        for (int index = 0; index < text.length(); ) {
            int codePoint = text.codePointAt(index);
            index += Character.charCount(codePoint);
            width += advance(codePoint);
        }
        return width;
    }

    // -------------------------------------------------------------------------------------------
    // Provider loading. The JSON shapes are the client's: a reference inlines another definition,
    // a space provider only declares advances, a bitmap provider slices a sheet by the code points
    // of its `chars` grid and a unihex provider reads a zipped Unifont .hex file.
    // -------------------------------------------------------------------------------------------

    private static List<Provider> readProviders(Renderer.ResourceStack resources, String definition, Set<String> options, int depth) throws IOException {
        byte[] data = resources.bytes(definition);
        if (data == null) {
            System.err.println("missing font definition " + definition + ", skipping it");
            return List.of();
        }
        if (depth > MAX_REFERENCE_DEPTH) {
            System.err.println("font definition " + definition + " nests too deep, skipping it");
            return List.of();
        }
        Map<String, Object> root;
        try {
            root = Json.object(Json.parse(new String(data, StandardCharsets.UTF_8)), definition);
        } catch (IllegalArgumentException error) {
            throw new IOException("invalid font definition " + definition + ": " + error.getMessage(), error);
        }
        if (!(root.get("providers") instanceof List<?> entries)) {
            return List.of();
        }
        List<Provider> providers = new ArrayList<>();
        for (Object entry : entries) {
            if (!(entry instanceof Map<?, ?> raw)) {
                continue;
            }
            Map<String, Object> provider = Json.object(raw, definition);
            if (!selected(provider.get("filter"), options)) {
                continue;
            }
            switch (Json.string(provider, "type", "")) {
                case "reference" -> providers.addAll(readProviders(resources, referencePath(Json.string(provider, "id", "")), options, depth + 1));
                case "space" -> providers.add(spaceProvider(provider));
                case "bitmap" -> providers.add(bitmapProvider(resources, provider));
                case "unihex" -> providers.add(unihexProvider(resources, provider));
                default -> System.err.println("unsupported font provider type " + Json.string(provider, "type", "") + " in " + definition);
            }
        }
        return providers;
    }

    /**
     * The client filter of a provider: every key of the filter must match whether that font option
     * is enabled. {@code default} renders with no option enabled, which keeps the bitmap sheets of
     * {@code include/default} ({@code uniform: false}) and drops the Japanese Unifont variant
     * ({@code jp: true}) the same way the game does for a Chinese client.
     */
    private static boolean selected(Object filter, Set<String> options) {
        if (!(filter instanceof Map<?, ?> raw) || raw.isEmpty()) {
            return true;
        }
        for (Map.Entry<?, ?> entry : raw.entrySet()) {
            boolean wanted = entry.getValue() instanceof Boolean value && value;
            if (wanted != options.contains(String.valueOf(entry.getKey()))) {
                return false;
            }
        }
        return true;
    }

    private static String referencePath(String id) {
        String path = id == null ? "" : id.trim();
        int separator = path.indexOf(':');
        if (separator >= 0) {
            path = path.substring(separator + 1);
        }
        return "assets/minecraft/font/" + path + ".json";
    }

    /** {@code font/...} resolves under {@code assets/<namespace>/}, the path a {@code hex_file} names. */
    private static String assetPath(String id) {
        String path = id == null ? "" : id.trim();
        String namespace = "minecraft";
        int separator = path.indexOf(':');
        if (separator >= 0) {
            namespace = path.substring(0, separator);
            path = path.substring(separator + 1);
        }
        return "assets/" + namespace + "/" + path;
    }

    /** {@code font/...} resolves under {@code assets/<namespace>/textures/}, the path a bitmap names. */
    private static String texturePath(String id) {
        String path = id == null ? "" : id.trim();
        String namespace = "minecraft";
        int separator = path.indexOf(':');
        if (separator >= 0) {
            namespace = path.substring(0, separator);
            path = path.substring(separator + 1);
        }
        return "assets/" + namespace + "/textures/" + path;
    }

    private static Provider spaceProvider(Map<String, Object> provider) {
        Map<Integer, Integer> advances = new HashMap<>();
        if (provider.get("advances") instanceof Map<?, ?> values) {
            for (Map.Entry<?, ?> entry : values.entrySet()) {
                String key = String.valueOf(entry.getKey());
                if (key.isEmpty() || !(entry.getValue() instanceof Number number)) {
                    continue;
                }
                advances.put(key.codePointAt(0), Math.max(0, (int) Math.round(number.doubleValue())));
            }
        }
        return new SpaceProvider(advances);
    }

    private static Provider bitmapProvider(Renderer.ResourceStack resources, Map<String, Object> provider) throws IOException {
        String file = Json.string(provider, "file", "");
        BufferedImage sheet = resources.image(texturePath(file));
        if (sheet == null) {
            System.err.println("missing font sheet " + file + ", skipping it");
            return new EmptyProvider();
        }
        List<String> rows = new ArrayList<>();
        if (provider.get("chars") instanceof List<?> entries) {
            for (Object entry : entries) {
                rows.add(String.valueOf(entry));
            }
        }
        int columns = 1;
        for (String row : rows) {
            columns = Math.max(columns, row.codePointCount(0, row.length()));
        }
        int cellWidth = Math.max(1, sheet.getWidth() / columns);
        int cellHeight = Math.max(1, sheet.getHeight() / Math.max(1, rows.size()));
        int ascent = Json.integer(provider, "ascent", cellHeight - 1);
        Map<Integer, Integer> advances = new HashMap<>();
        if (provider.get("advances") instanceof Map<?, ?> values) {
            for (Map.Entry<?, ?> entry : values.entrySet()) {
                String key = String.valueOf(entry.getKey());
                if (!key.isEmpty() && entry.getValue() instanceof Number number) {
                    advances.put(key.codePointAt(0), (int) Math.round(number.doubleValue()));
                }
            }
        }
        return new BitmapProvider(sheet, rows, cellWidth, cellHeight, ascent, advances);
    }

    private static Provider unihexProvider(Renderer.ResourceStack resources, Map<String, Object> provider) throws IOException {
        String hexFile = Json.string(provider, "hex_file", "");
        byte[] data = resources.bytes(assetPath(hexFile));
        if (data == null) {
            System.err.println("missing unihex file " + hexFile + ", skipping it");
            return new EmptyProvider();
        }
        List<int[]> overrides = new ArrayList<>();
        if (provider.get("size_overrides") instanceof List<?> entries) {
            for (Object entry : entries) {
                if (entry instanceof Map<?, ?> raw) {
                    Map<String, Object> override = Json.object(raw, hexFile);
                    String from = Json.string(override, "from", "");
                    String to = Json.string(override, "to", "");
                    if (from.isEmpty() || to.isEmpty()) {
                        continue;
                    }
                    overrides.add(new int[]{
                            from.codePointAt(0),
                            to.codePointAt(0),
                            Json.integer(override, "left", 0),
                            Json.integer(override, "right", 15)});
                }
            }
        }
        Map<Integer, int[]> bitmaps = new HashMap<>();
        try (ZipInputStream zip = new ZipInputStream(new ByteArrayInputStream(data))) {
            ZipEntry entry;
            while ((entry = zip.getNextEntry()) != null) {
                if (entry.getName().endsWith(".hex")) {
                    readUnifont(zip, bitmaps);
                }
            }
        }
        return new UnihexProvider(bitmaps, overrides);
    }

    /**
     * Reads one Unifont hex stream into left aligned 32 bit rows. The client accepts rows of 8, 16,
     * 24 or 32 pixels and shifts each one so the leftmost pixel sits in bit 31, which is what makes
     * the {@code size_overrides} columns absolute.
     */
    private static void readUnifont(java.io.InputStream input, Map<Integer, int[]> bitmaps) throws IOException {
        java.io.BufferedReader reader = new java.io.BufferedReader(new java.io.InputStreamReader(input, StandardCharsets.US_ASCII));
        String line;
        while ((line = reader.readLine()) != null) {
            int separator = line.indexOf(':');
            if (separator <= 0) {
                continue;
            }
            int codePoint;
            try {
                codePoint = Integer.parseInt(line.substring(0, separator).trim(), 16);
            } catch (NumberFormatException error) {
                continue;
            }
            String bits = line.substring(separator + 1).trim();
            int pixels = bits.length() / 4;
            if (!(pixels == 8 || pixels == 16 || pixels == 24 || pixels == 32) || bits.length() != pixels * 4) {
                continue;
            }
            int[] rows = new int[16];
            int shift = 32 - pixels;
            int perRow = pixels / 4;
            for (int row = 0; row < 16; row++) {
                long value = 0;
                for (int digit = 0; digit < perRow; digit++) {
                    int index = row * perRow + digit;
                    if (index >= bits.length()) {
                        break;
                    }
                    value = (value << 4) | hexDigit(bits.charAt(index));
                }
                rows[row] = (int) value << shift;
            }
            bitmaps.put(codePoint, rows);
        }
    }

    private static int hexDigit(char value) {
        if (value >= '0' && value <= '9') {
            return value - '0';
        }
        if (value >= 'a' && value <= 'f') {
            return value - 'a' + 10;
        }
        if (value >= 'A' && value <= 'F') {
            return value - 'A' + 10;
        }
        return 0;
    }

    // -------------------------------------------------------------------------------------------
    // Providers
    // -------------------------------------------------------------------------------------------

    private interface Provider {
        /** The glyph of this provider, or null when it does not cover the code point. */
        Glyph glyph(int codePoint);
    }

    private static final class EmptyProvider implements Provider {
        public Glyph glyph(int codePoint) {
            return null;
        }
    }

    private record SpaceProvider(Map<Integer, Integer> advances) implements Provider {
        public Glyph glyph(int codePoint) {
            Integer advance = advances.get(codePoint);
            return advance == null ? null : new Glyph(advance, 0, 0, 0, new byte[0]);
        }
    }

    /** A bitmap sheet: each code point of the {@code chars} grid selects one cell of the image. */
    private static final class BitmapProvider implements Provider {
        private final BufferedImage sheet;
        private final int cellWidth;
        private final int cellHeight;
        private final int ascent;
        private final Map<Integer, Integer> advances;
        private final Map<Integer, int[]> cells = new HashMap<>();
        private final Map<Integer, Glyph> glyphs = new HashMap<>();

        BitmapProvider(BufferedImage sheet, List<String> rows, int cellWidth, int cellHeight, int ascent, Map<Integer, Integer> advances) {
            this.sheet = sheet;
            this.cellWidth = cellWidth;
            this.cellHeight = cellHeight;
            this.ascent = ascent;
            this.advances = advances;
            for (int row = 0; row < rows.size(); row++) {
                String line = rows.get(row);
                int column = 0;
                for (int index = 0; index < line.length(); ) {
                    int codePoint = line.codePointAt(index);
                    index += Character.charCount(codePoint);
                    if (codePoint != 0) {
                        cells.put(codePoint, new int[]{column, row});
                    }
                    column++;
                }
            }
        }

        public Glyph glyph(int codePoint) {
            int[] cell = cells.get(codePoint);
            if (cell == null) {
                return null;
            }
            return glyphs.computeIfAbsent(codePoint, key -> read(key, cell[0] * cellWidth, cell[1] * cellHeight));
        }

        /**
         * Slices one cell. The client keeps the cell origin and trims only the empty columns on the
         * right, so a glyph keeps its left bearing; the advance is that width plus the one pixel gap,
         * unless the provider declares an explicit advance.
         */
        private Glyph read(int codePoint, int originX, int originY) {
            int width = 0;
            for (int column = cellWidth - 1; column >= 0; column--) {
                boolean inked = false;
                for (int row = 0; row < cellHeight; row++) {
                    if (pixel(originX + column, originY + row)) {
                        inked = true;
                        break;
                    }
                }
                if (inked) {
                    width = column + 1;
                    break;
                }
            }
            byte[] coverage = new byte[width * cellHeight];
            for (int row = 0; row < cellHeight; row++) {
                for (int column = 0; column < width; column++) {
                    if (pixel(originX + column, originY + row)) {
                        coverage[row * width + column] = 4;
                    }
                }
            }
            return new Glyph(advanceOf(codePoint, width), ascent, width, cellHeight, coverage);
        }
        private int advanceOf(int codePoint, int width) {
            Integer advance = advances.get(codePoint);
            return advance == null ? width + 1 : advance;
        }

        private boolean pixel(int x, int y) {
            if (x < 0 || y < 0 || x >= sheet.getWidth() || y >= sheet.getHeight()) {
                return false;
            }
            return (sheet.getRGB(x, y) >>> 24) != 0;
        }
    }

    /**
     * The {@code unihex} provider of Unifont. Its bitmap is sixteen texels tall and the client draws
     * it at half scale, so one glyph covers eight pixels of the eight pixel text cell with its top
     * edge on the ascent line, one pixel below the baseline. Every output pixel therefore sits on a
     * two by two block of texels and the share of lit texels in that block becomes its coverage: a
     * one texel stroke stays visible as a faint pixel instead of being voted away, and the glyph keeps
     * the half pixel edge the client has at GUI scale one.
     */
    private static final class UnihexProvider implements Provider {
        private static final int CELL = 8;

        private final Map<Integer, int[]> bitmaps;
        private final List<int[]> overrides;
        private final Map<Integer, Glyph> glyphs = new HashMap<>();

        UnihexProvider(Map<Integer, int[]> bitmaps, List<int[]> overrides) {
            this.bitmaps = bitmaps;
            this.overrides = overrides;
        }

        public Glyph glyph(int codePoint) {
            return glyphs.computeIfAbsent(codePoint, this::read);
        }

        private Glyph read(int codePoint) {
            int[] bitmap = bitmaps.get(codePoint);
            if (bitmap == null) {
                return null;
            }
            int ink = 0;
            for (int row = 0; row < bitmap.length; row++) {
                ink |= bitmap[row];
            }
            int left;
            int right;
            int[] override = overrideOf(codePoint);
            if (override != null) {
                left = override[2];
                right = override[3];
            } else if (ink == 0) {
                left = 0;
                right = 15;
            } else {
                left = Integer.numberOfLeadingZeros(ink);
                right = 32 - Integer.numberOfTrailingZeros(ink) - 1;
            }
            int texels = right - left + 1;
            int width = (texels + 1) / 2;
            byte[] coverage = new byte[width * CELL];
            for (int row = 0; row < CELL; row++) {
                for (int column = 0; column < width; column++) {
                    int lit = 0;
                    int seen = 0;
                    for (int dy = 0; dy < 2; dy++) {
                        int sourceY = row * 2 + dy;
                        if (sourceY >= 16) {
                            continue;
                        }
                        for (int dx = 0; dx < 2; dx++) {
                            int sourceX = left + column * 2 + dx;
                            if (sourceX > right) {
                                continue;
                            }
                            seen++;
                            if ((bitmap[sourceY] >> (31 - sourceX) & 1) != 0) {
                                lit++;
                            }
                        }
                    }
                    if (seen > 0) {
                        // Round the share of lit texels to the nearest quarter, so a fully inked
                        // block stays opaque and a single lit texel stays faintly visible.
                        coverage[row * width + column] = (byte) ((lit * 4 + seen / 2) / seen);
                    }
                }
            }
            // The texel grid is kept alongside the averaged cell: a half scale glyph loses half its
            // resolution when it is flattened to eight pixels, and magnifying that average is what
            // turned Chinese at twice the size into blocks of grey. The rows below hold the same
            // window, left aligned, so a device pixel can be covered by exactly one texel.
            int[] grid = new int[16];
            for (int row = 0; row < 16; row++) {
                grid[row] = bitmap[row] << left;
            }
            return new Glyph(texels / 2 + 1, ASCENT, width, CELL, coverage, grid, texels, 2);
        }

        private int[] overrideOf(int codePoint) {
            for (int[] override : overrides) {
                if (codePoint >= override[0] && codePoint <= override[1]) {
                    return override;
                }
            }
            return null;
        }
    }

    // -------------------------------------------------------------------------------------------
    // Glyphs
    // -------------------------------------------------------------------------------------------

    /** One drawable glyph: its advance, the baseline offset of its top edge and its pixel rows. */
    static final class Glyph {
        private final int advance;
        private final int ascent;
        private final int width;
        private final int height;
        /**
         * Per pixel coverage in quarters, from 0 (empty) to 4 (fully inked). The bitmap sheets of the
         * vanilla font only ever answer 0 or 4, while a {@code unihex} glyph is sixteen texels tall
         * and is painted at half scale, so a pixel covering one or two of its four texels is semi
         * transparent — the soft edge the game has at GUI scale one.
         */
        private final byte[] coverage;
        /**
         * The native texel rows of the glyph, or null when the provider paints whole pixels. A
         * {@code unihex} glyph is two texels per font pixel, so drawing it at twice the size can
         * reach the sixteen texel bitmap instead of magnifying the eight pixel average.
         */
        private final int[] texels;
        /** How many texels {@link #texels} describes, left aligned at bit 31. */
        private final int texelColumns;
        /** Texels per font pixel: 2 for a {@code unihex} bitmap, 0 when there is no texel grid. */
        private final int texelScale;

        Glyph(int advance, int ascent, int width, int height, byte[] coverage) {
            this(advance, ascent, width, height, coverage, null, 0, 0);
        }

        Glyph(int advance, int ascent, int width, int height, byte[] coverage, int[] texels, int texelColumns, int texelScale) {
            this.advance = advance;
            this.ascent = ascent;
            this.width = width;
            this.height = height;
            this.coverage = coverage;
            this.texels = texels;
            this.texelColumns = texelColumns;
            this.texelScale = texelScale;
        }

        /** The hollow five by seven box the client draws for a code point no provider covers. */
        static Glyph box(int advance, int height) {
            byte[] coverage = new byte[5 * height];
            for (int row = 0; row < height; row++) {
                for (int column = 0; column < 5; column++) {
                    if (row == 0 || row == height - 1 || column == 0 || column == 4) {
                        coverage[row * 5 + column] = 4;
                    }
                }
            }
            return new Glyph(advance, ASCENT, 5, height, coverage);
        }

        int advance() {
            return advance;
        }

        int ascent() {
            return ascent;
        }

        int width() {
            return width;
        }

        int height() {
            return height;
        }

        /** How much of the pixel at {@code column}, {@code row} is inked, in quarters from 0 to 4. */
        int coverage(int column, int row) {
            if (column < 0 || column >= width || row < 0 || row >= height) {
                return 0;
            }
            int index = row * width + column;
            return index < coverage.length ? coverage[index] : 0;
        }

        /** Whether a pixel is at least half covered: the pixels a half scale downsampling fills. */
        boolean inked(int column, int row) {
            return coverage(column, row) >= 2;
        }

        /**
         * Paints the glyph with its top edge at {@code top} and its left bearing at {@code x}, both
         * in the layout space the caller draws in.
         *
         * <p>{@code layoutScale} is how many layout units one font pixel occupies — the text scale of
         * the surface, one for a tooltip label and two for the tab list. The caller's Graphics2D
         * already carries the render scale, so nothing here multiplies by it.</p>
         *
         * <p>{@code deviceScale} is how many device pixels one font pixel ends up covering, which is
         * only what a texel grid is resampled at: at twice the size one texel lands on one device
         * pixel, so a Chinese character is the Unifont bitmap itself instead of the eight pixel
         * average magnified. Everything else keeps the coverage path, the half scale look the client
         * has at GUI scale one.</p>
         */
        void draw(Graphics2D graphics, int x, int top, int layoutScale, int deviceScale, int rgb, boolean italic) {
            if (texels != null && deviceScale >= texelScale) {
                drawTexels(graphics, x, top, layoutScale, deviceScale, rgb, italic);
            } else {
                drawCoverage(graphics, x, top, layoutScale, rgb, italic);
            }
        }

        /**
         * The half scale look: one averaged pixel per font pixel, run length merged, an italic glyph
         * sheared a quarter pixel per pixel of height and a partly covered pixel composited with a
         * matching alpha.
         */
        private void drawCoverage(Graphics2D graphics, int x, int top, int scale, int rgb, boolean italic) {
            Color[] palette = new Color[5];
            for (int level = 1; level <= 4; level++) {
                palette[level] = new Color(rgb & 0xFFFFFF | level * 255 / 4 << 24, true);
            }
            for (int row = 0; row < height; row++) {
                int shear = italic ? Math.round((ascent - row) * 0.25F * scale) : 0;
                int column = 0;
                while (column < width) {
                    int level = coverage(column, row);
                    if (level == 0) {
                        column++;
                        continue;
                    }
                    int run = column;
                    while (run < width && coverage(run, row) == level) {
                        run++;
                    }
                    graphics.setColor(palette[level]);
                    graphics.fillRect(x + column * scale + shear, top + row * scale, (run - column) * scale, scale);
                    column = run;
                }
            }
        }

        /**
         * Resamples the texel grid at the drawn resolution: every device pixel answers the share of
         * its own area that lit texels cover. At {@code deviceScale == texelScale} each texel lands on
         * exactly one pixel, so the glyph is the bitmap the font ships; at a scale between two texel
         * multiples the edge is averaged once, at the resolution it is drawn at, instead of
         * magnifying the average taken at half scale.
         *
         * <p>A device pixel can be smaller than one layout unit, so the rows are painted as
         * fractional rectangles: their edges land exactly on device pixel boundaries and the caller's
         * transform turns them back into whole device pixels.</p>
         */
        private void drawTexels(Graphics2D graphics, int x, int top, int layoutScale, int deviceScale, int rgb, boolean italic) {
            int deviceWidth = width * deviceScale;
            int deviceHeight = height * deviceScale;
            double span = (double) texelScale / deviceScale;
            double step = (double) layoutScale / deviceScale;
            double area = span * span;
            double[] ink = new double[deviceWidth];
            int[] alphas = new int[deviceWidth];
            Color[] palette = new Color[256];
            for (int row = 0; row < deviceHeight; row++) {
                double rowLow = row * span;
                double rowHigh = rowLow + span;
                java.util.Arrays.fill(ink, 0);
                int firstRow = Math.max(0, (int) Math.floor(rowLow));
                int lastRow = Math.min(texels.length, (int) Math.ceil(rowHigh));
                for (int texelRow = firstRow; texelRow < lastRow; texelRow++) {
                    double rowWeight = Math.min(rowHigh, texelRow + 1) - Math.max(rowLow, texelRow);
                    if (rowWeight <= 0) {
                        continue;
                    }
                    int bits = texels[texelRow];
                    for (int column = 0; column < deviceWidth; column++) {
                        double columnLow = column * span;
                        double columnHigh = columnLow + span;
                        double lit = 0;
                        int firstColumn = Math.max(0, (int) Math.floor(columnLow));
                        int lastColumn = Math.min(texelColumns, (int) Math.ceil(columnHigh));
                        for (int texelColumn = firstColumn; texelColumn < lastColumn; texelColumn++) {
                            double columnWeight = Math.min(columnHigh, texelColumn + 1) - Math.max(columnLow, texelColumn);
                            if (columnWeight > 0 && (bits >>> (31 - texelColumn) & 1) != 0) {
                                lit += columnWeight;
                            }
                        }
                        if (lit > 0) {
                            ink[column] += lit * rowWeight;
                        }
                    }
                }
                for (int column = 0; column < deviceWidth; column++) {
                    alphas[column] = ink[column] <= 0 ? 0 : (int) Math.min(255, Math.round(ink[column] / area * 255));
                }
                double shear = italic ? Math.round((ascent * deviceScale - row) * 0.25F) * step : 0;
                int column = 0;
                while (column < deviceWidth) {
                    int alpha = alphas[column];
                    if (alpha == 0) {
                        column++;
                        continue;
                    }
                    int run = column + 1;
                    while (run < deviceWidth && alphas[run] == alpha) {
                        run++;
                    }
                    if (palette[alpha] == null) {
                        palette[alpha] = new Color(rgb & 0xFFFFFF | alpha << 24, true);
                    }
                    graphics.setColor(palette[alpha]);
                    graphics.fill(new Rectangle2D.Double(x + column * step + shear, top + row * step,
                            (run - column) * step, step));
                    column = run;
                }
            }
        }
    }
}
