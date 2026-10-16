package com.illtamer.minecraft;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.IOException;
import java.util.Map;

/** One-shot JSON stdin to PNG stdout process used by the Go service. */
public final class Main {
    private Main() {}

    public static void main(String[] args) {
        try {
            Object value = Json.parse(System.in);
            Map<String, Object> request = Json.object(value, "request");
            BufferedImage image = new Renderer().render(request);
            if (!ImageIO.write(image, "png", System.out)) {
                throw new IOException("PNG writer is unavailable");
            }
            System.out.flush();
        } catch (Exception error) {
            System.err.println("minecraft-renderer: " + error.getMessage());
            System.exit(1);
        }
    }
}
