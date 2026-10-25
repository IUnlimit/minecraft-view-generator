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
 * <p>A definition is merged across the pack stack, again like the client: every copy the packs ship
 * contributes its providers, the highest pack first and the vanilla jar last. A pack that replaces
 * {@code default.json} with its own provider list therefore adds glyphs instead of having to copy
 * the vanilla includes, which is what ItemsAdder packs rely on.</p>
 *
 * <p>Metrics follow the client as well. Every provider sits on the seven pixel ascent the {@code
 * fwq} interface defines, bitmap glyphs are eight (or, for the accented sheet, twelve) pixels tall
 * starting at their own ascent, and a {@code unihex} glyph is sixteen texels drawn at half scale, so
 * it occupies eight pixels of the eight pixel cell with its top on the ascent line. A {@code bitmap}
 * provider that declares a {@code height} other than its sheet cell size is drawn at that height
 * instead — the client's default when the field is absent is eight pixels — so the glyph box is
 * {@code height} tall and the sheet is resampled into it. Advances are the per glyph values the
 * client computes: the last inked column plus one for a bitmap glyph and half the inked column count
 * plus one for a {@code unihex} glyph.</p>
 *
 * <p>A glyph is stored as per pixel coverage rather than as a bitmask. Sheets paint full pixels, but
 * the half scale of the {@code unihex} provider leaves most CJK edges covering only one or two of
 * their four texels, and keeping those as greys is what stops Chinese labels from turning into blocks
 * of solid ink.</p>
 *
 * <p>The colour of each pixel is kept as well. Every drawn pixel is the colour of the sheet times the
 * text colour, the modulation the client's font shader does: the vanilla sheets are pure white and
 * answer the text colour, while an icon a resource pack ships with colours of its own keeps them.</p>
 */
final class MinecraftFont {
    /** The line ascent every provider aligns to, {@code fwq.a} in the client. */
    static final int ASCENT = 7;
    /** The line height of the client font, used by the tooltip and chat layouts. */
    static final int LINE_HEIGHT = 9;

    private static final String DEFAULT_DEFINITION = "assets/minecraft/font/default.json";
    private static final int MAX_REFERENCE_DEPTH = 8;
    /**
     * The height a {@code bitmap} provider draws its glyphs at when it declares none. The client
     * defaults to the eight pixel text cell whatever resolution the sheet has, which is why an HD
     * font pack has to declare its own height to stay sharp.
     */
    private static final int DEFAULT_GLYPH_HEIGHT = 8;
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

    /**
     * Every provider the stack declares for one font definition, highest priority pack first.
     *
     * <p>The client merges a definition across the pack stack instead of letting the winning pack
     * replace it: each copy contributes its {@code providers} in turn, the highest pack first and the
     * vanilla jar last. That is what makes a pack able to replace {@code default.json} with nothing
     * but its own bitmap providers — an ItemsAdder pack does exactly that — while ASCII and CJK keep
     * coming from the {@code include/default} and {@code include/unifont} references of the copies
     * below it. Reading only the winning copy is what turned every label into empty boxes.</p>
     *
     * <p>{@code reference} recursion walks the stack the same way, so a pack can include its own
     * definition and still inherit the vanilla one underneath.</p>
     */
    private static List<Provider> readProviders(Renderer.ResourceStack resources, String definition, Set<String> options, int depth) throws IOException {
        List<byte[]> copies = resources.copies(definition);
        if (copies.isEmpty()) {
            System.err.println("missing font definition " + definition + ", skipping it");
            return List.of();
        }
        if (depth > MAX_REFERENCE_DEPTH) {
            System.err.println("font definition " + definition + " nests too deep, skipping it");
            return List.of();
        }
        List<Provider> providers = new ArrayList<>();
        for (byte[] data : copies) {
            appendProviders(resources, definition, data, options, depth, providers);
        }
        return providers;
    }

    /**
     * Appends the providers of one copy of a definition to {@code providers}, keeping the order the
     * client keeps: the first declaration of a code point wins.
     *
     * <p>A copy that does not parse only loses its own providers. One pack with a broken {@code
     * default.json} must not take the labels of the whole picture down with it, so the failure is a
     * warning naming the file and the rest of the stack still draws.</p>
     */
    private static void appendProviders(Renderer.ResourceStack resources, String definition, byte[] data, Set<String> options, int depth, List<Provider> providers) throws IOException {
        Map<String, Object> root;
        try {
            root = Json.object(Json.parse(new String(data, StandardCharsets.UTF_8)), definition);
        } catch (IllegalArgumentException error) {
            System.err.println("invalid font definition " + definition + " in one pack, skipping that copy: " + error.getMessage());
            return;
        }
        if (!(root.get("providers") instanceof List<?> entries)) {
            return;
        }
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
        // The height is the size of the resulting glyph in font pixels, not the size of the sheet
        // cell: a definition that leaves it out means the client's default of eight however large
        // the texture is, so a pack ships a two times resolution sheet by declaring half the cell.
        int height = Json.integer(provider, "height", 0);
        if (height <= 0) {
            height = DEFAULT_GLYPH_HEIGHT;
        }
        int ascent = Json.integer(provider, "ascent", height - 1);
        Map<Integer, Integer> advances = new HashMap<>();
        if (provider.get("advances") instanceof Map<?, ?> values) {
            for (Map.Entry<?, ?> entry : values.entrySet()) {
                String key = String.valueOf(entry.getKey());
                if (!key.isEmpty() && entry.getValue() instanceof Number number) {
                    advances.put(key.codePointAt(0), (int) Math.round(number.doubleValue()));
                }
            }
        }
        return new BitmapProvider(sheet, rows, cellWidth, cellHeight, height, ascent, advances);
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
        /**
         * The height the glyph is drawn at, in font pixels: the {@code height} of the definition or
         * the client's default of eight. A sheet whose cell is taller than this is resampled into the
         * smaller box, which is how a pack ships a two times resolution glyph at half its cell size.
         */
        private final int glyphHeight;
        private final int ascent;
        /** Texels per font pixel: {@code cellHeight / glyphHeight}. */
        private final double scale;
        private final Map<Integer, Integer> advances;
        private final Map<Integer, int[]> cells = new HashMap<>();
        private final Map<Integer, Glyph> glyphs = new HashMap<>();

        BitmapProvider(BufferedImage sheet, List<String> rows, int cellWidth, int cellHeight, int glyphHeight,
                       int ascent, Map<Integer, Integer> advances) {
            this.sheet = sheet;
            this.cellWidth = cellWidth;
            this.cellHeight = cellHeight;
            this.glyphHeight = glyphHeight;
            this.ascent = ascent;
            this.scale = (double) cellHeight / glyphHeight;
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
         *
         * <p>A cell whose texel size differs from the declared {@code height} becomes one glyph box of
         * font pixels: {@code height} tall and {@code ceil(trimmed / scale)} wide, its coverage the
         * share of inked texels per box pixel. The native texel grid travels with that glyph, so a
         * device scale of {@code scale} or more draws the sheet itself instead of the average taken
         * for the eight pixel text cell. A cell that is already {@code height} tall keeps the plain
         * one pixel per texel glyph, which is every vanilla sheet.</p>
         */
        private Glyph read(int codePoint, int originX, int originY) {
            int trimmed = 0;
            for (int column = cellWidth - 1; column >= 0; column--) {
                boolean inked = false;
                for (int row = 0; row < cellHeight; row++) {
                    if (pixel(originX + column, originY + row)) {
                        inked = true;
                        break;
                    }
                }
                if (inked) {
                    trimmed = column + 1;
                    break;
                }
            }
            if (scale == 1.0) {
                byte[] coverage = new byte[trimmed * cellHeight];
                int[] ink = new int[trimmed * cellHeight];
                boolean tinted = false;
                for (int row = 0; row < cellHeight; row++) {
                    for (int column = 0; column < trimmed; column++) {
                        int texel = texel(originX + column, originY + row);
                        if (texel == 0) {
                            continue;
                        }
                        coverage[row * trimmed + column] = 4;
                        ink[row * trimmed + column] = texel & 0xFFFFFF;
                        tinted |= (texel & 0xFFFFFF) != 0xFFFFFF;
                    }
                }
                // A white sheet is drawn in the text colour, which is every vanilla sheet: such a
                // glyph keeps no colour table at all.
                return new Glyph(advanceOf(codePoint, trimmed), ascent, trimmed, cellHeight, coverage,
                        tinted ? ink : null);
            }
            int[] texels = new int[trimmed * cellHeight];
            for (int row = 0; row < cellHeight; row++) {
                for (int column = 0; column < trimmed; column++) {
                    texels[row * trimmed + column] = texel(originX + column, originY + row);
                }
            }
            int width = (int) Math.ceil(trimmed / scale);
            Resampled resampled = resample(texels, trimmed, cellHeight, width, glyphHeight, scale);
            return new Glyph(advanceOf(codePoint, width), ascent, width, glyphHeight, resampled.coverage(),
                    resampled.ink(), texels, trimmed, cellHeight, scale);
        }

        /** The coverage and colour of a resampled glyph box. */
        private record Resampled(byte[] coverage, int[] ink) {}

        /**
         * Box filters the texel cell into the coverage of one font pixel per box pixel, in quarters:
         * a box pixel answers the share of its own area that inked texels cover, and the part of the
         * box that reaches past the trimmed cell counts as empty. At a scale of two a two by two texel
         * block therefore lands exactly on one pixel of the halved glyph. The colour of each box pixel
         * is the inked share's average, and the colour table is dropped entirely when the cell is
         * pure white.
         */
        private static Resampled resample(int[] texels, int texelWidth, int texelHeight, int width, int height, double scale) {
            byte[] coverage = new byte[width * height];
            int[] ink = new int[width * height];
            boolean tinted = false;
            double area = scale * scale;
            for (int row = 0; row < height; row++) {
                double rowLow = row * scale;
                double rowHigh = rowLow + scale;
                int firstRow = Math.max(0, (int) Math.floor(rowLow));
                int lastRow = Math.min(texelHeight, (int) Math.ceil(rowHigh));
                for (int column = 0; column < width; column++) {
                    double columnLow = column * scale;
                    double columnHigh = columnLow + scale;
                    int firstColumn = Math.max(0, (int) Math.floor(columnLow));
                    int lastColumn = Math.min(texelWidth, (int) Math.ceil(columnHigh));
                    double lit = 0;
                    double red = 0;
                    double green = 0;
                    double blue = 0;
                    for (int texelRow = firstRow; texelRow < lastRow; texelRow++) {
                        double rowWeight = Math.min(rowHigh, texelRow + 1) - Math.max(rowLow, texelRow);
                        if (rowWeight <= 0) {
                            continue;
                        }
                        for (int texelColumn = firstColumn; texelColumn < lastColumn; texelColumn++) {
                            double columnWeight = Math.min(columnHigh, texelColumn + 1) - Math.max(columnLow, texelColumn);
                            if (columnWeight <= 0) {
                                continue;
                            }
                            int texel = texels[texelRow * texelWidth + texelColumn];
                            double weight = columnWeight * rowWeight * (texel >>> 24) / 255.0;
                            if (weight <= 0) {
                                continue;
                            }
                            lit += weight;
                            red += weight * (texel >> 16 & 0xFF);
                            green += weight * (texel >> 8 & 0xFF);
                            blue += weight * (texel & 0xFF);
                        }
                    }
                    if (lit <= 0) {
                        continue;
                    }
                    coverage[row * width + column] = (byte) Math.min(4, Math.round(lit / area * 4));
                    int colour = (int) Math.round(red / lit) << 16
                            | (int) Math.round(green / lit) << 8
                            | (int) Math.round(blue / lit);
                    ink[row * width + column] = colour;
                    tinted |= colour != 0xFFFFFF;
                }
            }
            return new Resampled(coverage, tinted ? ink : null);
        }

        private int advanceOf(int codePoint, int width) {
            Integer advance = advances.get(codePoint);
            return advance == null ? width + 1 : advance;
        }

        private boolean pixel(int x, int y) {
            return texel(x, y) != 0;
        }

        /**
         * One texel of the sheet as straight (un premultiplied) {@code 0xAARRGGBB}, zero when it is
         * blank: the colour table of a glyph is built from these, so a sheet that ships colours of its
         * own keeps them.
         */
        private int texel(int x, int y) {
            if (x < 0 || y < 0 || x >= sheet.getWidth() || y >= sheet.getHeight()) {
                return 0;
            }
            int argb = sheet.getRGB(x, y);
            return (argb >>> 24) == 0 ? 0 : argb;
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
            // turned Chinese at twice the size into blocks of grey. It holds the same window as the
            // coverage, one texel per cell, so a device pixel can be covered by exactly one texel.
            int rows = bitmap.length;
            int[] grid = new int[rows * texels];
            for (int row = 0; row < rows; row++) {
                for (int column = 0; column < texels; column++) {
                    if ((bitmap[row] >> (31 - (left + column)) & 1) != 0) {
                        grid[row * texels + column] = 0xFFFFFFFF;
                    }
                }
            }
            return new Glyph(texels / 2 + 1, ASCENT, width, CELL, coverage, null, grid, texels, rows, 2);
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
         * The colour of each font pixel as {@code 0xRRGGBB}, or null when the glyph paints its whole
         * box white — every vanilla sheet and every {@code unihex} glyph. A drawn pixel is that colour
         * times the text colour, which is how the client draws a sheet that ships colours of its own:
         * a white pixel takes the text colour as it is, and a coloured one keeps its hue at the
         * brightness that colour allows.
         */
        private final int[] ink;
        /**
         * The native texel rows of the glyph as {@code 0xAARRGGBB}, or null when the glyph has no
         * resolution above its own pixels. A {@code unihex} glyph is two texels per font pixel and a
         * {@code bitmap} glyph whose sheet cell is taller than its declared height can be anything
         * from half to many texels per pixel, so drawing at a large enough device scale resamples
         * this grid instead of magnifying the coverage taken for the eight pixel cell.
         */
        private final int[] texels;
        /** How many texels each row of {@link #texels} holds. */
        private final int texelWidth;
        /** How many rows {@link #texels} holds. */
        private final int texelHeight;
        /** Texels per font pixel, and the device scale from which the grid is drawn directly. */
        private final double texelScale;

        Glyph(int advance, int ascent, int width, int height, byte[] coverage) {
            this(advance, ascent, width, height, coverage, null);
        }

        Glyph(int advance, int ascent, int width, int height, byte[] coverage, int[] ink) {
            this(advance, ascent, width, height, coverage, ink, null, 0, 0, 0);
        }

        Glyph(int advance, int ascent, int width, int height, byte[] coverage, int[] ink,
              int[] texels, int texelWidth, int texelHeight, double texelScale) {
            this.advance = advance;
            this.ascent = ascent;
            this.width = width;
            this.height = height;
            this.coverage = coverage;
            this.ink = ink;
            this.texels = texels;
            this.texelWidth = texelWidth;
            this.texelHeight = texelHeight;
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
        void draw(Graphics2D graphics, int x, int top, int layoutScale, int deviceScale, int rgb, boolean italic, boolean bold) {
            if (texels != null && deviceScale >= texelScale) {
                drawTexels(graphics, x, top, layoutScale, deviceScale, rgb, italic, bold);
            } else {
                drawCoverage(graphics, x, top, layoutScale, rgb, italic, bold);
            }
        }

        /**
         * The half scale look: one averaged pixel per font pixel, run length merged, an italic glyph
         * sheared a quarter pixel per pixel of height and a partly covered pixel composited with a
         * matching alpha. A glyph that carries colours of its own is painted pixel by pixel instead,
         * each one that colour times the text colour, because two neighbours of the same coverage can
         * still differ in hue.
         */
        private void drawCoverage(Graphics2D graphics, int x, int top, int scale, int rgb, boolean italic, boolean bold) {
            Color[] palette = new Color[5];
            for (int level = 1; level <= 4; level++) {
                palette[level] = new Color(rgb & 0xFFFFFF | level * 255 / 4 << 24, true);
            }
            // A bold glyph paints one column past its own box: that is exactly the pixel the second,
            // one pixel offset pass used to add, and it would be lost at the last column of the cell.
            int columns = width + (bold ? 1 : 0);
            for (int row = 0; row < height; row++) {
                int shear = italic ? Math.round((ascent - row) * 0.25F * scale) : 0;
                if (ink != null) {
                    for (int column = 0; column < columns; column++) {
                        int coverage = level(column, row, bold);
                        if (coverage == 0) {
                            continue;
                        }
                        graphics.setColor(new Color(modulate(inkAt(column, row), rgb) | coverage * 255 / 4 << 24, true));
                        graphics.fillRect(x + column * scale + shear, top + row * scale, scale, scale);
                    }
                    continue;
                }
                int column = 0;
                while (column < columns) {
                    int coverage = level(column, row, bold);
                    if (coverage == 0) {
                        column++;
                        continue;
                    }
                    int run = column;
                    while (run < columns && level(run, row, bold) == coverage) {
                        run++;
                    }
                    graphics.setColor(palette[coverage]);
                    graphics.fillRect(x + column * scale + shear, top + row * scale, (run - column) * scale, scale);
                    column = run;
                }
            }
        }

        /**
         * The colour this pixel carries, {@code 0xFFFFFF} when the glyph paints none: a blank pixel and
         * the column a bold glyph widens past its own box both fall back to the text colour, the wide
         * column taking the hue of the pixel it repeats.
         */
        private int inkAt(int column, int row) {
            if (ink == null || row < 0 || row >= height) {
                return 0xFFFFFF;
            }
            int source = column;
            if (column < 0 || column >= width || coverage(column, row) == 0) {
                source = column - 1;
            }
            if (source < 0 || source >= width) {
                return 0xFFFFFF;
            }
            int value = ink[row * width + source];
            return value == 0 ? 0xFFFFFF : value;
        }

        /**
         * Multiplies the colour of one glyph pixel into the text colour, which is how the client draws
         * a sheet that ships colours of its own: a white pixel takes the text colour as it is, and a
         * coloured one keeps its hue at the brightness the text colour allows.
         */
        static int modulate(int texel, int rgb) {
            if (texel == 0xFFFFFF) {
                return rgb;
            }
            int red = ((rgb >> 16 & 0xFF) * (texel >> 16 & 0xFF)) / 255;
            int green = ((rgb >> 8 & 0xFF) * (texel >> 8 & 0xFF)) / 255;
            int blue = ((rgb & 0xFF) * (texel & 0xFF)) / 255;
            return red << 16 | green << 8 | blue;
        }

        /**
         * How inked one font pixel is when the glyph is drawn, in quarters. A bold glyph is the same
         * ink grown by one pixel to the right — the maximum of this pixel and its left neighbour, so
         * a stroke widens without ever becoming darker than a plain one. Drawing the glyph twice
         * instead would composite the same coverage twice and add brightness steps that a plain
         * glyph never has, which is exactly the double outline bold used to show.
         */
        private int level(int column, int row, boolean bold) {
            int level = coverage(column, row);
            return bold ? Math.max(level, coverage(column - 1, row)) : level;
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
        private void drawTexels(Graphics2D graphics, int x, int top, int layoutScale, int deviceScale, int rgb, boolean italic, boolean bold) {
            int deviceWidth = (width + (bold ? 1 : 0)) * deviceScale;
            int deviceHeight = height * deviceScale;
            double span = texelScale / deviceScale;
            double step = (double) layoutScale / deviceScale;
            double area = span * span;
            boolean tinted = ink != null;
            double[] weights = new double[deviceWidth];
            double[] reds = tinted ? new double[deviceWidth] : null;
            double[] greens = tinted ? new double[deviceWidth] : null;
            double[] blues = tinted ? new double[deviceWidth] : null;
            int[] alphas = new int[deviceWidth];
            Color[] palette = new Color[256];
            for (int row = 0; row < deviceHeight; row++) {
                double rowLow = row * span;
                double rowHigh = rowLow + span;
                java.util.Arrays.fill(weights, 0);
                if (tinted) {
                    java.util.Arrays.fill(reds, 0);
                    java.util.Arrays.fill(greens, 0);
                    java.util.Arrays.fill(blues, 0);
                }
                int firstRow = Math.max(0, (int) Math.floor(rowLow));
                int lastRow = Math.min(texelHeight, (int) Math.ceil(rowHigh));
                for (int texelRow = firstRow; texelRow < lastRow; texelRow++) {
                    double rowWeight = Math.min(rowHigh, texelRow + 1) - Math.max(rowLow, texelRow);
                    if (rowWeight <= 0) {
                        continue;
                    }
                    int base = texelRow * texelWidth;
                    for (int column = 0; column < deviceWidth; column++) {
                        double columnLow = column * span;
                        double columnHigh = columnLow + span;
                        double lit = 0;
                        double red = 0;
                        double green = 0;
                        double blue = 0;
                        int firstColumn = Math.max(0, (int) Math.floor(columnLow));
                        int lastColumn = Math.min(texelWidth, (int) Math.ceil(columnHigh));
                        for (int texelColumn = firstColumn; texelColumn < lastColumn; texelColumn++) {
                            double columnWeight = Math.min(columnHigh, texelColumn + 1) - Math.max(columnLow, texelColumn);
                            if (columnWeight <= 0) {
                                continue;
                            }
                            int texel = texels[base + texelColumn];
                            double weight = columnWeight * (texel >>> 24) / 255.0;
                            if (weight <= 0) {
                                continue;
                            }
                            lit += weight;
                            if (tinted) {
                                red += weight * (texel >> 16 & 0xFF);
                                green += weight * (texel >> 8 & 0xFF);
                                blue += weight * (texel & 0xFF);
                            }
                        }
                        if (lit > 0) {
                            weights[column] += lit * rowWeight;
                            if (tinted) {
                                reds[column] += red * rowWeight;
                                greens[column] += green * rowWeight;
                                blues[column] += blue * rowWeight;
                            }
                        }
                    }
                }
                for (int column = 0; column < deviceWidth; column++) {
                    alphas[column] = weights[column] <= 0 ? 0 : (int) Math.min(255, Math.round(weights[column] / area * 255));
                }
                if (bold) {
                    // Grow the ink one font pixel to the right on the device grid, exactly like the
                    // coverage path: the maximum of a pixel and its left neighbour, never a second
                    // pass of the same alpha.
                    for (int column = deviceWidth - 1; column > 0; column--) {
                        alphas[column] = Math.max(alphas[column], alphas[column - 1]);
                    }
                }
                double shear = italic ? Math.round((ascent * deviceScale - row) * 0.25F) * step : 0;
                if (tinted) {
                    // Two neighbours of the same coverage can differ in hue, so a coloured glyph is
                    // painted pixel by pixel, its colour the inked share's average.
                    for (int column = 0; column < deviceWidth; column++) {
                        int alpha = alphas[column];
                        if (alpha == 0) {
                            continue;
                        }
                        int source = weights[column] > 0 ? column : column - 1;
                        double total = source < 0 ? 0 : weights[source];
                        if (total <= 0) {
                            continue;
                        }
                        int texel = (int) Math.round(reds[source] / total) << 16
                                | (int) Math.round(greens[source] / total) << 8
                                | (int) Math.round(blues[source] / total);
                        graphics.setColor(new Color(modulate(texel, rgb) | alpha << 24, true));
                        graphics.fill(new Rectangle2D.Double(x + column * step + shear, top + row * step, step, step));
                    }
                    continue;
                }
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
