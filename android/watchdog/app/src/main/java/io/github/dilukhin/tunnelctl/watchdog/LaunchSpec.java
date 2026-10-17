package io.github.dilukhin.tunnelctl.watchdog;

final class LaunchSpec {
    static final String HOME = "/data/data/com.termux/files/home";
    static final String DEFAULT_BINARY = "/data/data/com.termux/files/usr/bin/tunnelctl";
    static final String DEFAULT_CONFIG = HOME + "/.config/tunnelctl/tunnelctl.json";
    final String binary;
    final String config;
    final String target;

    LaunchSpec(String binary, String config, String target) {
        if (target == null || !privatePath(binary) || !privatePath(config)
                || !binary.substring(binary.lastIndexOf('/') + 1).startsWith("tunnelctl")
                || !target.matches("[\\p{L}\\p{N}_][\\p{L}\\p{N}_.-]{0,79}")) {
            throw new IllegalArgumentException("Нужны пути внутри Termux и имя профиля или группы без пробелов.");
        }
        this.binary = binary;
        this.config = config;
        this.target = target;
    }

    private static boolean privatePath(String path) {
        if (path == null || path.length() > 1024 || path.endsWith("/")) return false;
        if (!path.startsWith("/data/data/com.termux/files/")
                && !path.startsWith("/data/user/0/com.termux/files/")) return false;
        for (int i = 0; i < path.length(); i++) if (Character.isISOControl(path.charAt(i))) return false;
        for (String part : path.split("/")) if (part.equals(".") || part.equals("..")) return false;
        return true;
    }

    String[] arguments() { return new String[]{"ensure", target, "--config", config}; }
}
