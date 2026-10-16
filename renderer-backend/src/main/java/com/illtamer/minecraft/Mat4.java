package com.illtamer.minecraft;

/**
 * Minimal row major 4x4 matrix used by the held item transform chain.
 *
 * <p>It mirrors the two vanilla helpers that build the third person item pose:
 * {@code com.mojang.math.Transformation} ({@code T * R_left * S * R_right}) and the
 * {@code net.minecraft.client.renderer.item.ItemTransform} display transform
 * ({@code T * rotationXYZ * S * T(-0.5, -0.5, -0.5)}), so the numbers below can be compared
 * against the decompiled 1.21.11 client one to one.</p>
 */
final class Mat4 {
    private final double[] value;

    private Mat4(double[] value) {
        this.value = value;
    }

    static Mat4 identity() {
        return new Mat4(new double[]{
                1, 0, 0, 0,
                0, 1, 0, 0,
                0, 0, 1, 0,
                0, 0, 0, 1
        });
    }

    static Mat4 translation(double x, double y, double z) {
        Mat4 result = identity();
        result.value[3] = x;
        result.value[7] = y;
        result.value[11] = z;
        return result;
    }

    static Mat4 scaling(double x, double y, double z) {
        Mat4 result = identity();
        result.value[0] = x;
        result.value[5] = y;
        result.value[10] = z;
        return result;
    }

    static Mat4 rotationX(double degrees) {
        double angle = Math.toRadians(degrees);
        double cos = Math.cos(angle);
        double sin = Math.sin(angle);
        return new Mat4(new double[]{
                1, 0, 0, 0,
                0, cos, -sin, 0,
                0, sin, cos, 0,
                0, 0, 0, 1
        });
    }

    static Mat4 rotationY(double degrees) {
        double angle = Math.toRadians(degrees);
        double cos = Math.cos(angle);
        double sin = Math.sin(angle);
        return new Mat4(new double[]{
                cos, 0, sin, 0,
                0, 1, 0, 0,
                -sin, 0, cos, 0,
                0, 0, 0, 1
        });
    }

    static Mat4 rotationZ(double degrees) {
        double angle = Math.toRadians(degrees);
        double cos = Math.cos(angle);
        double sin = Math.sin(angle);
        return new Mat4(new double[]{
                cos, -sin, 0, 0,
                sin, cos, 0, 0,
                0, 0, 1, 0,
                0, 0, 0, 1
        });
    }

    /**
     * Vanilla display transforms rotate with {@code new Quaternionf().rotationXYZ(x, y, z)}.
     * Verified against joml 1.10.8: that quaternion is exactly {@code Rx * Ry * Rz}.
     */
    static Mat4 rotationXyz(double x, double y, double z) {
        return rotationX(x).multiply(rotationY(y)).multiply(rotationZ(z));
    }

    /** The project coordinate change from the vanilla model space: {@code (x, y, z) -> (x, 24 - y, z)}. */
    static Mat4 projectSpace() {
        Mat4 result = identity();
        result.value[5] = -1;
        result.value[7] = 24;
        return result;
    }

    Mat4 multiply(Mat4 other) {
        double[] result = new double[16];
        for (int row = 0; row < 4; row++) {
            for (int column = 0; column < 4; column++) {
                double sum = 0;
                for (int index = 0; index < 4; index++) {
                    sum += value[row * 4 + index] * other.value[index * 4 + column];
                }
                result[row * 4 + column] = sum;
            }
        }
        return new Mat4(result);
    }

    double[] point(double x, double y, double z) {
        return new double[]{
                value[0] * x + value[1] * y + value[2] * z + value[3],
                value[4] * x + value[5] * y + value[6] * z + value[7],
                value[8] * x + value[9] * y + value[10] * z + value[11]
        };
    }

    /**
     * Transforms a normal with the inverse transpose of the linear block, which is what keeps
     * face normals perpendicular to their plane when the chain contains a mirror or non uniform scale.
     */
    double[] normal(double x, double y, double z) {
        double a = value[0], b = value[1], c = value[2];
        double d = value[4], e = value[5], f = value[6];
        double g = value[8], h = value[9], i = value[10];
        double determinant = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g);
        if (Math.abs(determinant) < 1e-12) {
            return new double[]{x, y, z};
        }
        // The inverse transpose equals the cofactor matrix divided by the determinant, and the
        // determinant cancels out again because the result is normalised below.
        double nx = (e * i - f * h) * x + (f * g - d * i) * y + (d * h - e * g) * z;
        double ny = (c * h - b * i) * x + (a * i - c * g) * y + (b * g - a * h) * z;
        double nz = (b * f - c * e) * x + (c * d - a * f) * y + (a * e - b * d) * z;
        double length = Math.sqrt(nx * nx + ny * ny + nz * nz);
        if (length < 1e-12) {
            return new double[]{x, y, z};
        }
        return new double[]{nx / length, ny / length, nz / length};
    }
}
