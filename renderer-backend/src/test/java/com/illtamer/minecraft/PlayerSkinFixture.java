package com.illtamer.minecraft;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.Base64;

/**
 * Deterministic 64x64 skin used by the smoke test. Every vanilla texture region is painted with
 * a distinct colour so the test can verify that the renderer samples the same rectangles as the
 * game: head, body, both arms, both legs and the second (outer) layer.
 */
final class PlayerSkinFixture {
    private PlayerSkinFixture() {}

    static final int HEAD = 0xC82828;
    static final int HAT = 0xF5F5F5;
    static final int BODY = 0x28B428;
    static final int RIGHT_ARM = 0xE6D228;
    static final int LEFT_ARM = 0x28BEE6;
    static final int RIGHT_LEG = 0xD228D2;
    static final int LEFT_LEG = 0xF07814;

    static BufferedImage image() {
        BufferedImage image = new BufferedImage(64, 64, BufferedImage.TYPE_INT_ARGB);
        fill(image, 0, 0, 32, 16, HEAD);
        fill(image, 32, 0, 64, 16, HAT);
        fill(image, 16, 16, 40, 32, BODY);
        fill(image, 40, 16, 56, 32, RIGHT_ARM);
        fill(image, 0, 16, 16, 32, RIGHT_LEG);
        fill(image, 32, 48, 48, 64, LEFT_ARM);
        fill(image, 16, 48, 32, 64, LEFT_LEG);
        return image;
    }

    static String png64() throws IOException {
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        ImageIO.write(image(), "png", output);
        return "data:image/png;base64," + Base64.getEncoder().encodeToString(output.toByteArray());
    }

    private static void fill(BufferedImage image, int x0, int y0, int x1, int y1, int color) {
        for (int y = y0; y < y1; y++) {
            for (int x = x0; x < x1; x++) {
                image.setRGB(x, y, 0xFF000000 | color);
            }
        }
    }
}
