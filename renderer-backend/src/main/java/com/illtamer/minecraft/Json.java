package com.illtamer.minecraft;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** A small strict JSON reader so the renderer has no runtime dependencies. */
final class Json {
    private Json() {}

    static Object parse(InputStream input) throws IOException {
        return parse(new String(input.readAllBytes(), StandardCharsets.UTF_8));
    }

    static Object parse(String text) {
        Parser parser = new Parser(text);
        Object value = parser.value();
        parser.skipWhitespace();
        if (!parser.atEnd()) {
            throw parser.error("trailing JSON data");
        }
        return value;
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> object(Object value, String name) {
        if (!(value instanceof Map<?, ?>)) {
            throw new IllegalArgumentException(name + " must be an object");
        }
        return (Map<String, Object>) value;
    }

    @SuppressWarnings("unchecked")
    static List<Object> array(Object value, String name) {
        if (!(value instanceof List<?>)) {
            throw new IllegalArgumentException(name + " must be an array");
        }
        return (List<Object>) value;
    }

    static String string(Map<String, Object> object, String key, String fallback) {
        Object value = object.get(key);
        return value instanceof String ? (String) value : fallback;
    }

    static boolean bool(Map<String, Object> object, String key, boolean fallback) {
        Object value = object.get(key);
        return value instanceof Boolean ? (Boolean) value : fallback;
    }

    static double number(Map<String, Object> object, String key, double fallback) {
        Object value = object.get(key);
        return value instanceof Number ? ((Number) value).doubleValue() : fallback;
    }

    static int integer(Map<String, Object> object, String key, int fallback) {
        Object value = object.get(key);
        return value instanceof Number ? ((Number) value).intValue() : fallback;
    }

    /** Compact serialisation used by the advanced tooltip component dump. */
    static String write(Object value) {
        StringBuilder builder = new StringBuilder();
        write(value, builder);
        return builder.toString();
    }

    private static void write(Object value, StringBuilder builder) {
        if (value == null) {
            builder.append("null");
            return;
        }
        if (value instanceof Map<?, ?> map) {
            builder.append('{');
            boolean first = true;
            for (Map.Entry<?, ?> entry : map.entrySet()) {
                if (!first) {
                    builder.append(',');
                }
                first = false;
                write(String.valueOf(entry.getKey()), builder);
                builder.append(':');
                write(entry.getValue(), builder);
            }
            builder.append('}');
            return;
        }
        if (value instanceof List<?> list) {
            builder.append('[');
            for (int index = 0; index < list.size(); index++) {
                if (index > 0) {
                    builder.append(',');
                }
                write(list.get(index), builder);
            }
            builder.append(']');
            return;
        }
        if (value instanceof String string) {
            write(string, builder);
            return;
        }
        builder.append(value);
    }

    private static void write(String value, StringBuilder builder) {
        builder.append('"');
        for (int index = 0; index < value.length(); index++) {
            char character = value.charAt(index);
            switch (character) {
                case '"' -> builder.append("\\\"");
                case '\\' -> builder.append("\\\\");
                case '\n' -> builder.append("\\n");
                case '\r' -> builder.append("\\r");
                case '\t' -> builder.append("\\t");
                default -> {
                    if (character < 0x20) {
                        builder.append(String.format("\\u%04x", (int) character));
                    } else {
                        builder.append(character);
                    }
                }
            }
        }
        builder.append('"');
    }

    private static final class Parser {
        private final String text;
        private int position;

        Parser(String text) {
            this.text = text;
        }

        boolean atEnd() {
            return position >= text.length();
        }

        void skipWhitespace() {
            while (!atEnd() && Character.isWhitespace(text.charAt(position))) {
                position++;
            }
        }

        Object value() {
            skipWhitespace();
            if (atEnd()) {
                throw error("expected a JSON value");
            }
            return switch (text.charAt(position)) {
                case '{' -> object();
                case '[' -> array();
                case '"' -> stringValue();
                case 't' -> literal("true", Boolean.TRUE);
                case 'f' -> literal("false", Boolean.FALSE);
                case 'n' -> literal("null", null);
                default -> numberValue();
            };
        }

        private Map<String, Object> object() {
            expect('{');
            LinkedHashMap<String, Object> result = new LinkedHashMap<>();
            skipWhitespace();
            if (consume('}')) {
                return result;
            }
            while (true) {
                skipWhitespace();
                if (atEnd() || text.charAt(position) != '"') {
                    throw error("object keys must be strings");
                }
                String key = stringValue();
                skipWhitespace();
                expect(':');
                Object value = value();
                if (result.putIfAbsent(key, value) != null) {
                    throw error("duplicate object key " + key);
                }
                skipWhitespace();
                if (consume('}')) {
                    return result;
                }
                expect(',');
            }
        }

        private List<Object> array() {
            expect('[');
            ArrayList<Object> result = new ArrayList<>();
            skipWhitespace();
            if (consume(']')) {
                return result;
            }
            while (true) {
                result.add(value());
                skipWhitespace();
                if (consume(']')) {
                    return result;
                }
                expect(',');
            }
        }

        private String stringValue() {
            expect('"');
            StringBuilder result = new StringBuilder();
            while (!atEnd()) {
                char current = text.charAt(position++);
                if (current == '"') {
                    return result.toString();
                }
                if (current < 0x20) {
                    throw error("control character in string");
                }
                if (current != '\\') {
                    result.append(current);
                    continue;
                }
                if (atEnd()) {
                    throw error("unterminated escape");
                }
                char escaped = text.charAt(position++);
                switch (escaped) {
                    case '"' -> result.append('"');
                    case '\\' -> result.append('\\');
                    case '/' -> result.append('/');
                    case 'b' -> result.append('\b');
                    case 'f' -> result.append('\f');
                    case 'n' -> result.append('\n');
                    case 'r' -> result.append('\r');
                    case 't' -> result.append('\t');
                    case 'u' -> result.append(unicodeEscape());
                    default -> throw error("invalid string escape");
                }
            }
            throw error("unterminated string");
        }

        private char unicodeEscape() {
            if (position + 4 > text.length()) {
                throw error("short unicode escape");
            }
            String hex = text.substring(position, position + 4);
            position += 4;
            try {
                return (char) Integer.parseInt(hex, 16);
            } catch (NumberFormatException error) {
                throw error("invalid unicode escape");
            }
        }

        private Object numberValue() {
            int start = position;
            if (consume('-')) {
                if (atEnd()) {
                    throw error("invalid number");
                }
            }
            if (consume('0')) {
                if (!atEnd() && Character.isDigit(text.charAt(position))) {
                    throw error("leading zero in number");
                }
            } else {
                if (atEnd() || !Character.isDigit(text.charAt(position))) {
                    throw error("invalid number");
                }
                while (!atEnd() && Character.isDigit(text.charAt(position))) {
                    position++;
                }
            }
            boolean decimal = false;
            if (consume('.')) {
                decimal = true;
                if (atEnd() || !Character.isDigit(text.charAt(position))) {
                    throw error("invalid number fraction");
                }
                while (!atEnd() && Character.isDigit(text.charAt(position))) {
                    position++;
                }
            }
            if (!atEnd() && (text.charAt(position) == 'e' || text.charAt(position) == 'E')) {
                decimal = true;
                position++;
                if (!atEnd() && (text.charAt(position) == '+' || text.charAt(position) == '-')) {
                    position++;
                }
                if (atEnd() || !Character.isDigit(text.charAt(position))) {
                    throw error("invalid number exponent");
                }
                while (!atEnd() && Character.isDigit(text.charAt(position))) {
                    position++;
                }
            }
            String number = text.substring(start, position);
            try {
                if (decimal) {
                    return Double.parseDouble(number);
                }
                return Long.parseLong(number);
            } catch (NumberFormatException error) {
                throw error("invalid number");
            }
        }

        private Object literal(String literal, Object result) {
            if (!text.startsWith(literal, position)) {
                throw error("invalid literal");
            }
            position += literal.length();
            return result;
        }

        private boolean consume(char expected) {
            if (!atEnd() && text.charAt(position) == expected) {
                position++;
                return true;
            }
            return false;
        }

        private void expect(char expected) {
            if (!consume(expected)) {
                throw error("expected '" + expected + "'");
            }
        }

        IllegalArgumentException error(String message) {
            return new IllegalArgumentException(message + " at byte " + position);
        }
    }
}
