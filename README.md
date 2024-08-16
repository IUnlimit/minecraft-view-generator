# Minecraft 视图生成器
This project is an http server used to generate Minecraft view files for other services. 该项目是一个 http 服务器，用于为其他服务生成 Minecraft 视图文件。

## 支持的功能

- 资源获取
  - [x] 客户端材质
  - [x] 正版玩家皮肤
- 字体渲染
  - [x] 原版字体(支持中文)
- 2D UV 贴图绘制
  - [x] PlayerList
  - [ ] Inventory
  - [ ] Book
  - [ ] Map
  - [ ] Server Banner
  - [ ] ItemMeta

## 功能展示

### PlayerList

![](./docs/images/player_list.png)

## Swagger

```shell
go run main.go
# See http://localhost:{port}/api/v1/swagger/index.html
```