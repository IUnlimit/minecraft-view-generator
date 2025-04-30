# Minecraft 视图生成器
This project is an http server used to generate Minecraft view files for other services. 该项目是一个 http 服务器，用于为其他服务生成 Minecraft 视图文件。

## 支持的功能

- 资源获取
  - [x] 客户端材质
  - [x] 正版玩家皮肤
  - [ ] Blessing Skin
- 字体渲染
  - [x] 原版字体(支持中文)
- 2D UV 贴图绘制
  - [x] PlayerList
  - [ ] Inventory
    - [ ] PlayerInventory
    - [ ] EnderChest
  - [ ] Item
    - [ ] ItemMeta
    - [ ] Book
    - [ ] Map
  - [ ] Advancement
  - [ ] Server Banner

## 功能展示

### PlayerList

![](./docs/images/player_list.png)

## Swagger

```shell
go run main.go
# See http://localhost:{port}/api/v1/swagger/index.html
```

## Questions

### raylib(No such file or directory)

```shell
# github.com/gen2brain/raylib-go/raylib
In file included from ./external/glfw/src/platform.h:81,
                 from ./external/glfw/src/internal.h:325,
                 from ./external/glfw/src/context.c:28,
                 from /home/illtamer/go/pkg/mod/github.com/gen2brain/raylib-go/raylib@v0.0.0-20240807111636-8861ee437da9/cgo_linux.go:7:
./external/glfw/src/x11_platform.h:48:10: fatal error: X11/extensions/XInput2.h: No such file or directory
   48 | #include <X11/extensions/XInput2.h>
      |          ^~~~~~~~~~~~~~~~~~~~~~~~~~
compilation terminated.
```

尝试安装所有缺少的库

```shell
sudo dnf install wayland-devel libxkbcommon-x11-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel mesa-libGL-devel glfw-devel
```

### raylib(WARNING: GLFW: Error: 65550 Description: Failed to detect any supported platform)

此报错是因为运行环境无显示器，尝试使用以下列指令指定 sdl 代替 glfw

```shell
SDL_VIDEODRIVER=offscreen go run -tags sdl .
```