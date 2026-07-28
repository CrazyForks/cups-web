# CUPS Web 开发者指南

本文档面向开发者，介绍项目架构、API、开发流程与扩展方式。用户文档请参阅 [README.md](README.md)。

## 📦 项目概述

- **项目定位**：基于 CUPS 的 Web 打印管理工具，前后端分离
- **技术栈**：Go 1.26（后端）+ Vue 3（前端）+ SQLite（存储）+ IPP（打印协议）
- **部署形态**：
  - 单二进制（前端通过 `go:embed` 打包进可执行文件），连接外部 CUPS
  - **单容器（AIO）Docker 镜像**：`cupsd` 与 `cups-web` 跑在同一个容器里，内置 LibreOffice + Java 21 + OFD 转换器 + Ghostscript + 打印驱动生态。`entrypoint.sh` 后台拉起 cupsd（带 watchdog），前台 `exec /cups-web` 作为 PID 1

> ⚠️ **历史形态提示**：仓库曾经是「cups 镜像 + cups-web 镜像」双容器（`cups/Dockerfile` + `cups/entrypoint.sh` + `cups/scripts/*`），整个 `cups/` 目录已在合并提交里删除。现在只有根目录的一份 `Dockerfile` / `entrypoint.sh`，构建脚本在 `scripts/build/`、驱动脚本在 `scripts/driver/`。看到旧文档/旧 issue 提到 `cups/` 路径时按此对应。

## 🛠️ 技术栈

### 后端

| 组件 | 版本 / 说明 |
| --- | --- |
| Go | 1.26（见 `go.mod`） |
| HTTP 路由 | `github.com/gorilla/mux` |
| 会话管理 | `github.com/gorilla/securecookie` |
| 数据库 | `modernc.org/sqlite`（纯 Go，无 CGO） |
| 打印协议 | `github.com/OpenPrinting/goipp`（IPP） |
| PDF 解析 | `rsc.io/pdf`（页数读取）、`github.com/phpdave11/gofpdf`（PDF 生成） |
| 图像缩放 | `golang.org/x/image/draw`（CatmullRom，用于大图下采样） |
| 加密 | `golang.org/x/crypto/bcrypt` |

### 前端

| 组件 | 版本 / 说明 |
| --- | --- |
| 框架 | Vue 3.5 + Vue Router（hash 模式） |
| 构建 | Vite 7 |
| UI 库 | `@nuxt/ui` v4（含自带的 Tailwind 主题） |
| 样式 | Tailwind CSS v4 |
| 图标 | `@iconify-json/lucide` |
| PDF 处理 | `pdfjs-dist`（预览，PDF 生成统一交由后端 `/api/convert`） |
| HEIC 兼容 | `heic2any` |
| 包管理 | 本地开发推荐 Bun（`bun install` / `bun run dev`）；CI 与 Docker 镜像统一用 npm（`npm ci` + `npm run build`），以同时覆盖 `linux/arm/v7` 架构——Bun 官方不支持 32-bit ARM（见部署章节） |

### 外部依赖

| 依赖 | 作用 |
| --- | --- |
| CUPS | 打印服务，通过 IPP 通信。AIO 镜像里由 `scripts/build/install-cups.sh` 从 OpenPrinting 源码编译（当前 `CUPS_VERSION=2.4.19`）后 overlay 覆盖 apt 版 |
| LibreOffice（headless） | Office 文档 → PDF；同时作为 PDF 标准化的兜底链路。**依赖可写 `HOME`**：Dockerfile 显式 `ENV HOME=/root` + 预建 `~/.config/libreoffice`，拿不到可写 HOME 时 `--convert-to pdf` 会返回 0 但不产出 PDF（静默失败，极难排查） |
| Java 21 + `ofd-converter.jar` | OFD 文档 → PDF（基于 `ofdrw`）。构建期 `openjdk-21-jdk-headless`，运行期 `openjdk-21-jre` |
| Ghostscript (`gs`) | PDF 标准化首选链路：统一降级到 PDF 1.4 兼容性（主要面向 CUPS/老打印机对新版 PDF 解析能力弱的场景）。**注意：`gs pdfwrite` 会对原 PDF 的每个字体对象强行加上 subset 前缀（`CCGWER+` 之类 6 位随机码）并重建字体字典**，对"空壳 Type0 字体 + `UniGB-UCS2-H` 外部 CMap"（Acrobat 导出的准考证/国标表格最常见的形态）是**破坏性改造**：原 PDF 的 `/BaseFont /#ba#da#cc#e5`（宋体 GBK 字节转义）会被改写成 `/BaseFont /BPCXJX+#cb#ce#cc#e5`，让 pdf.js 等渲染器误以为有内嵌字形可用、走内嵌路径却拿不到真实 FontFile，字宽表 vs 字形度量对不上导致"先正确一闪、再错位挤压"。因此该链路**不是 PDF 预览乱码的解药**，只在 CUPS 驱动确认无法解析原字体字典时才有收益。本地 macOS 需要 `brew install ghostscript`；Docker 镜像里给 gs 配了**三层中文字体兜底**（见 `Dockerfile` 注释）：①`docker-fonts/cidfmap.local` 把 GBK 字节 BaseFont（宋/黑/楷/仿宋 × Regular/Bold，共 8 条）精准映射到 `arphic-uming` / `arphic-ukai` / `wqy-zenhei` 这三套**纯 TrueType**字体（用户放了 `simsun.ttf` 等 Windows 字体时构建期 `sed` 换成真实字体），构建期同时装到 `/etc/ghostscript/cidfmap.local` 与 gs 的 `Resource/Init/cidfmap`（后者被 gs 启动时自动加载，详见下文 cidfmap 小节）；②`fonts-droid-fallback` 作为 cidfmap 未命中时的 Adobe-GB1 CID 兜底（Debian 把 gs 依赖的 `DroidSansFallback.ttf` 剥离到独立包）；③`fonts-noto-cjk` 等 Unicode 字形包仅服务 LibreOffice 渲染 Office 文档，不参与 CIDFSubst 路径。之所以只用 arphic/wqy 而不用 Noto CJK OTC，是因为 gs 10.x 对 CFF-based OpenType Collection 的 CIDFont 子字体索引偶有坑，纯 TrueType 最稳。|
| `dpkg` / `apt-get` | 运行时安装第三方打印驱动（见「驱动管理」章节）。⚠️ runtime 镜像**不含 `dpkg-dev`**，所以脚本里只能用 `dpkg --print-architecture`，不能用 `dpkg-architecture` |

## 📁 项目结构

```text
cups-web/
├── cmd/server/                    # 后端主程序
│   ├── main.go                    # 入口与路由注册（含 /api/admin/drivers/*）
│   ├── app.go                     # 全局变量（appStore、uploadDir）
│   ├── bootstrap.go               # 默认 admin 初始化
│   ├── auth_handlers.go           # 登录 / 登出 / session / csrf；writeJSON / writeJSONStatus / writeJSONError / randomToken
│   ├── login_limiter.go           # 登录失败限流
│   ├── admin_handlers.go          # 管理员：用户 / 系统设置 / 手动清理
│   ├── user_handlers.go           # /api/me
│   ├── print_handlers.go          # /api/print（主打印入口）
│   ├── print_records_handlers.go  # 打印记录查询、文件下载、重新打印
│   ├── printer_info_handler.go    # 打印机属性查询（IPP Get-Printer-Attributes）
│   ├── convert_handler.go         # /api/convert（文档 → PDF 转换）
│   ├── convert_utils.go           # 调 LibreOffice / OFD 转换器的工具
│   ├── compose_handler.go         # /api/compose（多页拼版）
│   ├── estimate_handler.go        # /api/estimate（预估页数）
│   ├── driver_handlers.go         # /api/admin/drivers/*：列表 / 安装 / 卸载 / 检测 / 上传 / 一键设置 + 后台任务系统
│   ├── driver_registry.go         # 驱动注册表（DriverMeta / DriverStatus / currentDebArch / matchDriverForPrinter）
│   ├── file_utils.go              # 文件保存、文件类型识别、页数统计
│   ├── pdf_utils.go               # 图片 / 文本 → PDF 的渲染
│   ├── pdf_compose.go             # 多页拼版（compose）
│   ├── pdf_reorder.go             # 页序重排（even-reverse 等）+ 测试
│   ├── watermark.go               # 水印绘制
│   ├── pdf_normalize.go           # PDF 标准化管线（gs → LibreOffice → passthrough）+ cidfmapPreambleArgs
│   ├── pdf_normalize_test.go      # PDF 标准化相关的本地测试用例
│   ├── fonts.go                   # 中文字体加载（内嵌 assets/fonts）
│   ├── maintenance.go             # 后台维护任务（按保留天数清理）
│   ├── version.go                 # 构建期注入的版本号（-ldflags -X main.Version）
│   └── assets/fonts/              # 打包进二进制的字体资源
├── internal/
│   ├── auth/session.go            # securecookie 会话 + CSRF cookie
│   ├── middleware/
│   │   ├── csrf.go                # RequireSession / RequireAdmin / ValidateCSRF
│   │   └── security.go            # SecurityHeaders / CrossOriginProtection
│   ├── ipp/
│   │   ├── client.go              # IPP 客户端：列表、属性（含 stateDurationSeconds）、提交打印
│   │   └── security.go            # 打印机 URI 校验
│   ├── server/static.go           # 静态资源嵌入服务（SPA fallback）
│   └── store/                     # 数据层
│       ├── store.go               # DB 打开 + 迁移
│       ├── users.go               # users CRUD
│       ├── prints.go              # print_jobs CRUD
│       └── settings.go            # settings KV 存取
├── frontend/
│   ├── embed.go                   # go:embed dist/** → frontend.FS
│   ├── src/
│   │   ├── main.js                # Vue app 入口
│   │   ├── App.vue                # 顶层布局：header（打印 / 驱动 / 管理导航） / router-view / footer
│   │   ├── router/index.js        # hash 路由 + session 缓存守卫
│   │   ├── views/                 # LoginView / PrintView / AdminView / DriversView（驱动管理页）
│   │   ├── components/print/      # 业务组件（FileUpload / PdfCanvas / PrintOptions / …）
│   │   ├── polyfills/             # Promise.withResolvers、Uint8Array base64、pdf worker
│   │   ├── utils/                 # api / file / format / image 工具
│   │   └── index.css              # 全局样式
│   ├── public/pdfjs/              # 离线 pdf.js cmaps / 标准字体
│   ├── package.json               # 依赖清单（package-lock.json 与 bun.lock 并存，见部署章节）
│   └── vite.config.js
├── ofd-converter/                 # Java 子项目：OFD → PDF
│   ├── pom.xml
│   └── src/
├── scripts/
│   ├── build/
│   │   └── install-cups.sh        # 源码编译 OpenPrinting/cups（CUPS_VERSION=2.4.19），只在 cups-builder 阶段跑
│   └── driver/                    # 驱动管理命令 + 各驱动安装脚本（运行时按需执行，见「驱动管理」章节）
│       ├── driver-install.sh      # → /usr/local/bin/driver-install：文件系统 diff + manifest + 持久化
│       ├── driver-list.sh         # → /usr/local/bin/driver-list：CLI 列出可用/已装驱动
│       ├── driver-remove.sh       # → /usr/local/bin/driver-remove：按 manifest 删文件（带白名单安全网）
│       ├── restore-drivers.sh     # → /usr/local/bin/restore-drivers：启动时按 manifest 恢复
│       ├── install-canon-ufr2.sh      # Canon UFR II/UFRII LT 官方 .deb（amd64 + arm64；其他架构 exit 3）
│       ├── install-canon-capt.sh      # Canon CAPT LBP2900 开源驱动（源码编译，全架构）
│       ├── install-hp-laserjet1020.sh # HP 1020 固件 sihp1020.dl + A4-default PPD 变体
│       ├── install-foo2zjs-firmware.sh# HP 1000/1005/1018/P100x/P1505 固件（编译 arm2hpdl）
│       ├── install-escpr2.sh      # Epson ESC/P-R 2（amd64/armhf 预编译 deb，其他架构源码编译）
│       ├── install-epson-cn.sh    # Epson 国行专有 .deb（仅 amd64；其他架构 exit 3）
│       ├── install-konica-bizhub.sh   # 柯尼卡美能达 bizhub 3000MF（amd64 + arm64）
│       ├── install-sharp.sh       # Sharp MX-C2622R PostScript PPD（全架构）
│       └── install-gutenprint.sh  # printer-driver-gutenprint（armhf/armel exit 3）
├── docker-fonts/                  # 构建期注入的字体与 gs/fontconfig 配置
│   ├── cidfmap.local              # gs GBK 字节 BaseFont → TrueType 映射（8 条）
│   ├── fontconfig-chinese.conf    # LibreOffice 用的中文字体别名
│   └── sim{sun,hei,kai,fang}.ttf  # 可选的 Windows 中文字体（用户自备则替换 arphic/wqy 映射）
├── entrypoint.sh                  # AIO 容器启动脚本（restore-drivers → cupsd watchdog → exec /cups-web）
├── .github/workflows/             # CI：二进制交叉编译发布 + 单个多架构镜像构建
├── .drivers/                      # 运行时驱动快照（.gitignore，compose 挂到 /opt/cups-drivers/data）
├── .data/ .uploads/ .etc/         # 运行时数据库 / 上传 / CUPS 配置（.gitignore）
├── Dockerfile                     # 单容器（CUPS + Web）五阶段构建
├── docker-compose.yml             # 单个 cups 服务（AIO）
├── Makefile                       # 构建脚本
├── bump-version.sh                # 语义化版本打 tag 脚本
├── go.mod / go.sum
├── README.md                      # 用户文档
└── AGENTS.md                      # 本文档
```

## 🔌 HTTP API

所有接口以 `/api` 为前缀。除登录/登出/csrf/session 外的接口均需通过 `RequireSession` 与 `ValidateCSRF` 两个中间件；管理员接口再叠加 `RequireAdmin`。

> **CSRF 约定**：登录成功后服务端会下发 `csrf_token` Cookie（非 HttpOnly，前端可读）；前端在所有非 GET 请求上带 `X-CSRF-Token` 头，与 Cookie 值一致方可通过。

### 公开接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/login` | 账号密码登录，成功后下发 session + csrf cookie |
| POST | `/api/logout` | 清除 session 与 csrf cookie |
| GET | `/api/csrf` | 手动刷新 csrf token |
| GET | `/api/session` | 查询当前会话（未登录返回 401） |
| GET | `/api/version` | 返回二进制构建期通过 `-ldflags -X main.Version` 注入的版本号；前端 footer 展示用（Issue #26） |

### 已登录用户接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/me` | 当前用户信息（id / username / role） |
| GET | `/api/printers` | 列出 CUPS 中的打印机 |
| GET | `/api/printer-info?uri=<uri>` | 查询打印机属性（状态、队列任务数等） |
| POST | `/api/estimate` | 上传文件，返回估算页数 |
| POST | `/api/convert` | 上传文件，返回转换后的 PDF 流；支持单文件（`file` 字段，PDF / Office / OFD / 图片 / 文本）与多图合并（`files` 字段，多张图合成单个 PDF） |
| POST | `/api/print` | 提交打印任务（前端支持批量模式：选择混合文件类型时逐个转换并提交） |
| POST | `/api/compose` | 多页拼版（`composeHandler` → `pdf_compose.go`） |
| GET | `/api/print-records` | 查询自己的打印记录（可带 `start` / `end`） |
| GET | `/api/print-records/{id}/file` | 下载打印记录对应的原始文件 |
| POST | `/api/print-records/{id}/reprint` | 读取该记录的完整打印参数快照，供前端预填「重新打印」对话框 |

### 管理员接口（`/api/admin/*`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/admin/users` | 列出所有用户 |
| POST | `/api/admin/users` | 创建用户 |
| PUT | `/api/admin/users/{id}` | 更新用户 |
| DELETE | `/api/admin/users/{id}` | 删除用户（`admin` 账号禁止） |
| GET | `/api/admin/print-records` | 查询全站打印记录（可带 `username` / `start` / `end`） |
| GET | `/api/admin/settings` | 读取系统设置 |
| PUT | `/api/admin/settings` | 更新系统设置（`retentionDays`） |
| POST | `/api/admin/cleanup` | 手动触发清理过期打印记录与文件（同维护任务逻辑） |
| GET | `/api/admin/drivers` | 列出注册表里所有驱动 + 安装状态 + 当前架构 + 已归档的自定义 `.deb` |
| POST | `/api/admin/drivers/install` | **异步**安装驱动，`202` + `jobId` |
| POST | `/api/admin/drivers/remove` | **异步**卸载驱动，`202` + `jobId` |
| GET | `/api/admin/drivers/detect` | `lpinfo -l -v` 扫描已连接打印机并推荐驱动 |
| POST | `/api/admin/drivers/upload` | 上传自定义 `.ppd` 或 `.deb`（**同步**，请求体硬上限 `driverUploadMaxBytes` = 64MB） |
| POST | `/api/admin/drivers/setup` | **异步**一键设置：装驱动 + `lpadmin` 建队列，`202` + `jobId` |
| GET | `/api/admin/drivers/jobs/{id}` | 轮询后台驱动任务的状态与增量日志 |

#### 驱动接口的异步任务模型（`driver_handlers.go`）

`install` / `remove` / `setup` 三个接口**必须是异步**的，不要"简化"回同步：

- `main.go` 的 `http.Server` 是全局 `WriteTimeout = 120s`，而编译型驱动（`canon-capt`、`foo2zjs-firmware`、arm64 上的 `escpr2`）在容器里现场 `apt-get install build-essential` + `make`，几分钟到十几分钟都正常。
- 同步实现里用 `exec.CommandContext(r.Context(), ...)`：连接一超时，请求 context 被 cancel，**`CommandContext` 会直接 kill 掉正在 `make` 的进程**，留下半编译产物（以及没被 EXIT trap 卸载干净的编译依赖），客户端还什么都拿不到。
- 现在的实现：handler 立刻 `202` 返回 `jobId`，真正的命令跑在 `context.Background()` 派生的 goroutine 里（硬超时常量 `driverJobTimeout = 30 * time.Minute`），前端轮询 `jobs/{id}` 拿增量日志。命令的 stdout/stderr 都写进加锁的 `safeBuffer`，所以轮询能看到"正在编译"的实时输出而不是等结束才一次性拿到。

**单飞（single-flight）**：`startDriverJob` 在锁内扫一遍任务表，**同一时刻只允许一个驱动任务在跑**——apt/dpkg 自身有全局锁，并发安装只会互相失败，报错还很难懂。已有任务运行中时接口返回 `409` 并带上正在跑的 `jobId`，前端可以直接切过去轮询：

```json
{ "error": "已有驱动任务正在执行，请等待其完成后重试", "jobId": "…" }
```

`.deb` 上传（同步执行 `dpkg -i`）也走同一把逻辑锁：`runningDriverJobID() != ""` 时直接 `409`，避免和后台任务抢 dpkg 锁。

**任务保留期**：`driverJobRetention = time.Hour`，`pruneDriverJobsLocked` 在每次新建任务时清掉完成超过 1 小时的旧任务，防止长期运行的进程无限累积（任务只存在内存里，进程重启即丢，前端已按此假设做超时提示）。`jobId` 是 `randomToken()` 生成的不透明大写 base32 串，路由约束因此放宽成 `{id:[A-Za-z0-9]+}`。

请求 / 响应形状（照代码实录）：

| 接口 | 请求体 | 成功响应 |
| --- | --- | --- |
| `POST /drivers/install`<br>`POST /drivers/remove` | `{"name": "<driver>"}` | `202 {"jobId": "…", "name": "<driver>"}` |
| `POST /drivers/setup` | `{"deviceUri": "usb://…", "driverName": "", "manufacturer": "", "model": ""}`（仅 `deviceUri` 必填） | `202 {"jobId": "…", "name": "<driverName>"}` |
| `GET /drivers/jobs/{id}` | — | `{"id","kind","name","status","log","error","startedAt","finishedAt","result"}`，`status ∈ {running, succeeded, failed}`；`kind ∈ {install, remove, setup}`；`setup` 成功时 `result = {"printerName","driverInstalled","ppdUsed"}` |
| `GET /drivers` | — | `{"currentArch","drivers":[…],"customDebs":[…],"customDebNotice":"…"}` |
| `GET /drivers/detect` | — | `[{"deviceUri","manufacturer","model","connection","driverMatch","hasDriver"}]`（无设备时是 `[]` 而非 `null`） |
| `POST /drivers/upload` | multipart `file`（`.ppd` / `.deb`） | `.ppd`：`{"ok":true,"type":"ppd","filename"}`；`.deb`：`{"ok":true,"type":"deb","filename","warning","log"}` |

`drivers[]` 中每项是 `DriverStatus`（`driver_registry.go`）：`name` / `displayName` / `description` / `arch` / `needCompile` / `installed` / `installedAt` / `installedArch` / `supported` / `hasScript`。

- `supported`：`driverSupportsArch(d, currentDebArch())`，前端据此禁用「安装」按钮
- `hasScript`：镜像里确实存在 `/opt/cups-drivers/scripts/install-<name>.sh`（注册表可能领先于脚本）
- `installed`：**以 `/opt/cups-drivers/data/<name>/manifest.txt` 是否存在为唯一判据**
- `installedAt` / `installedArch`：读 `metadata.txt` 的 `installed_at=` / `arch=`（历史代码读的 `date=` 键从来不存在，永远不命中；`installed_at` 缺失时退回 `manifest.txt` 的 mtime）

`customDebs[]` 中每项是 `CustomDebPackage`：`filename` / `installedAt`（用各自 `.deb` 文件的 mtime，比目录级 metadata 更准） / `installedArch` / `sizeBytes`。它是**纯信息性条目**，只为提示用户"装过什么、重启后要重装什么"。

### `/api/print` 表单字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `file` | file | 待打印文件（multipart） |
| `printer` | string | 打印机 URI |
| `duplex` | `"true"` / `"false"` | 是否双面 |
| `color` | `"true"` / `"false"` | 是否彩色 |
| `copies` | int | 份数 |
| `orientation` | `portrait` / `landscape` | 页面方向 |
| `paper_size` | `A4` / `A3` / `5inch` / `6inch` / `7inch` / `8inch` / `10inch` | 纸张尺寸 |
| `paper_type` | `plain` / `photo` / `glossy` / `matte` / `envelope` / `cardstock` / `labels` / `auto` | 纸张类型 |
| `media_source` | string（打印机上报的纸盒关键字，如 `tray-1` / `main` / `manual`） | 进纸盒（对应 IPP `media-source`，映射到 CUPS 驱动 PPD 的 `InputSlot`）；可选值由 `/api/printer-info` 返回的 `mediaSourceSupported` 动态决定，`auto`（默认）不发送到 IPP（Issue #75） |
| `print_scaling` | `auto` / `auto-fit` / `fit` / `fill` / `none` | 缩放策略 |
| `page_range` | string | 页码范围，如 `1-5 8 10-12` |
| `page_set` | `all` / `odd` / `even` | 页面子集（仅打奇数页 / 仅打偶数页）；在 `page_range` 截出的页序基础上再过滤，典型场景是**手动双面打印**——先打奇数页，把纸翻面放回后再打偶数页。对应 CUPS 的 `page-set` 属性（由 `pdftopdf` filter 处理），`all` 视为默认值、不会发送到 IPP 请求。前端留空或选「全部页」等同于 `all` |
| `mirror` | `"true"` / `"false"` | 镜像打印 |
| `number_up` | `1` / `2` / `4` / `6` / `9` / `16` | 一张多页（N-up），每张纸缩排的逻辑页数；`1`（默认）= 关闭，不发送到 IPP。由 CUPS `pdftopdf` filter 原生处理，对应 IPP `number-up`（Issue #78） |
| `number_up_layout` | `lrtb` / `rltb` / `tblr` / `tbrl` | N-up 的页面排布顺序（横向 Z 形 / 纵向 N 形），对应 IPP `number-up-layout`；仅 `number_up > 1` 时生效 |
| `page_border` | `single` / `none` | N-up 时是否为每个小页绘制边框，对应 IPP `page-border`；仅 `number_up > 1` 时生效 |

## 🗄️ 数据库

SQLite，启用 `WAL` + `foreign_keys`；迁移逻辑在 `internal/store/store.go` 的 `migrate()` 中，**使用幂等 SQL**，支持老库热升级（通过 `addColumnIfMissing` 增量加列）。

### `users`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER PK | 自增主键 |
| `username` | TEXT UNIQUE | 登录名 |
| `password_hash` | TEXT | bcrypt 哈希 |
| `role` | TEXT | `admin` / `user` |
| `protected` | INTEGER | `1` 表示受保护（默认 `admin` 账号） |
| `contact_name` | TEXT | 联系人 |
| `phone` | TEXT | 电话 |
| `email` | TEXT | 邮箱 |
| `created_at` / `updated_at` | TEXT | RFC3339 UTC |

### `print_jobs`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER PK | 自增主键 |
| `user_id` | INTEGER FK → users | 提交者 |
| `printer_uri` | TEXT | 目标打印机 URI |
| `filename` | TEXT | 原始文件名 |
| `stored_path` | TEXT | 相对 `uploads/` 的路径 |
| `pages` | INTEGER | 页数 |
| `job_id` | TEXT | IPP 返回的 Job ID |
| `status` | TEXT | `queued` / `printed` |
| `is_duplex` | INTEGER | 是否双面 |
| `is_color` | INTEGER | 是否彩色 |
| `copies` | INTEGER | 份数（Issue #68） |
| `orientation` | TEXT | 页面方向 `portrait` / `landscape`（Issue #68） |
| `paper_size` | TEXT | 纸张尺寸（Issue #68） |
| `paper_type` | TEXT | 纸张类型（Issue #68） |
| `media_source` | TEXT | 进纸盒关键字，`auto` = 自动（Issue #68） |
| `print_scaling` | TEXT | 缩放策略（Issue #68） |
| `page_range` | TEXT | 页码范围（Issue #68） |
| `page_set` | TEXT | 页面子集 `all` / `odd` / `even` / `even-reverse`（Issue #68） |
| `mirror` | INTEGER | 镜像打印（Issue #68） |
| `watermark_text` | TEXT | 水印文字（Issue #68） |
| `number_up` | INTEGER | 一张多页 N-up（Issue #68） |
| `number_up_layout` | TEXT | N-up 排布顺序（Issue #68） |
| `page_border` | TEXT | N-up 每小页边框 `single` / `none`（Issue #68） |
| `created_at` | TEXT | RFC3339 UTC |

> 💡 除 `is_duplex` / `is_color` 外，其余打印参数列均为 [Issue #68](https://github.com/hanxi/cups-web/issues/68) 新增：首次打印时把**完整打印参数**快照落库，`print_records_handlers.go::reprintHandler` 读取后让前端「重新打印」对话框（复用 `PrintOptions` 组件）**精确预填第一次的每一项设置**。老库经 `migrate()` 的 `addColumnIfMissing` 热升级，历史记录这些列取默认值（`A4` / `portrait` / `auto` / `all` 等），重打时退化为合理默认。注意 `page_set` 落库存的是用户原始选择（`even-reverse` 等），不是 even-reverse 重排后被改写的值。

### `settings`

KV 表：`key TEXT PRIMARY KEY` + `value TEXT`。

当前使用的键：

- `retention_days`：打印记录保留天数（`0` = 永久）
- `session_hash_key` / `session_block_key`：securecookie 的密钥（首次启动自动生成并持久化）

## 🔐 认证与安全

### 会话流程

1. **启动时**：`auth.SetupSecureCookie` 从 `settings` 读取 / 生成 `session_hash_key` + `session_block_key`（各 32 字节），构造 `securecookie.SecureCookie`
2. **登录**：校验密码后写入两条 cookie：
   - `session`（HttpOnly，加密+签名，编码 `{userId, username, role, expires}`）
   - `csrf_token`（非 HttpOnly，前端 JS 可读）
3. **鉴权中间件链**：
   - `RequireSession`：解出 session
   - `RequireAdmin`：再校验 `role == admin`
   - `ValidateCSRF`：对非 GET/HEAD/OPTIONS 请求比对 `X-CSRF-Token` 头与 cookie
4. **登出**：`ClearSession` 将两条 cookie 都设为 `MaxAge=-1`

### 默认管理员

`bootstrap.go::ensureDefaultAdmin` 保证始终存在一个 `admin` 用户：

- 若不存在：创建 `admin/admin` 且 `protected=1`
- 若存在但角色/保护位异常：纠正为 `admin` + `protected=1`
- 代码中通过 `Username == "admin"` 判定保护逻辑（禁止改名、改角色、删除）

## 🖨️ 打印流水

`printHandler`（`cmd/server/print_handlers.go`）是核心入口，流程：

1. **接收**：解析 multipart 表单（上限 512MB），提取 `file` + 打印参数
2. **落盘**：`saveUploadedFile` 将上传文件按日期分目录保存到 `uploads/YYYYMMDD/` 下，文件名做安全化处理
3. **类型识别 & 转换**（`detectFileKind`）：
   - `pdf` → **PDF 标准化管线**（`diagnosePDF` 诊断日志 → `normalizePDF`：Ghostscript `pdfwrite -dCompatibilityLevel=1.4 -dEmbedAllFonts=true` 优先（两档 strict `/prepress` → lenient `-dNEWPDF=false -dPDFSTOPONERROR=false` 重试）→ LibreOffice `--convert-to pdf` 兜底 → passthrough 最终降级）。**该管线只解决"CUPS 老驱动拒绝 PDF-1.7 新语法"这一类真正的兼容性故障**，对"预览显示"不会有帮助：gs 会把空壳 CJK 字体改写成带 subset 前缀的假嵌入字体，反而让浏览器 pdf.js 在预览时出现错位（详见前端 `PdfCanvas.vue` 的 `getDocument` 参数注释）。因此 `/api/convert` 预览入口应该**优先让 pdf.js 直接读原始 PDF**，只在真实打印前做最小化标准化。
   - `office` → `convertOfficeToPDF`（调 `libreoffice --headless --convert-to pdf`）
   - `ofd` → `convertOFDToPDF`（调 `java -jar /ofd-converter.jar`）
   - `image` → `convertImageToPDF`（用 `gofpdf` 渲染；长边超过 3000px 的大图会先经 `downscaleImageIfNeeded` 下采样到 3000px 并以 JPEG Q85 重编码再嵌入 PDF，避免把手机端 10MB+ 原图整张塞进 PDF 导致移动端预览/下载超时，见 [Issue #22](https://github.com/hanxi/cups-web/issues/22)；PNG 透明像素会被合成到白底以符合打印预期）
   - `text` → `convertTextToPDF`（用 `gofpdf` + 内嵌中文字体渲染）
4. **页数统计**：`countPDFPages` / `countPDFPagesWithFallback` / `estimateTextPages`；PDF 页数读取失败时走 `normalizePDF` 再重试，仍失败则以 1 页兜底而非直接 400
5. **持久化**：在 `print_jobs` 插入一条 `queued` 记录
6. **提交打印**：`ipp.SendPrintJob` 构造 `Print-Job` IPP 请求并发出
7. **回写状态**：成功后更新为 `printed` 并回填 `job_id`

转换或标准化后的 PDF 以 `<原文件>.print.pdf` 副文件形式存到 `uploads/`，维护任务清理时会连同原文件一起删除。`/api/convert` 对 PDF 也会走同一条 `normalizePDF` 管线，让前端 `PdfCanvas` 预览与最终打印使用完全相同的字节流。

> ⚠️ 已知副作用：Acrobat 导出的"空壳 Type0 + `UniGB-UCS2-H`"字体字典（`/BaseFont /#ba#da#cc#e5` 这种裸宋体名，准考证/国标表格常见）经 gs 改写为"subset 前缀 + FontFile2 假内嵌"后（`/BaseFont /CCGWER+#ba#da#cc#e5`），**pdf.js** 预览会出现"每 3-4 字错 1 字"的挤压错位（浏览器原生 PDF 引擎因有系统字体兜底不受影响）。之所以仍然共用 `normalizePDF`，是因为"预览与打印看到同一份字节流"的一致性比这类特殊 PDF 的预览准确性更重要——前端只使用 `pdfjs-dist` 在 canvas 里渲染预览（见 `frontend/src/components/print/PdfCanvas.vue`），遇到上述错位时用户可以忽略，不影响打印。

### Ghostscript cidfmap：中文字形映射的两套加载机制

打印纸面中文字形的配套：镜像把 `docker-fonts/cidfmap.local` 里宋/黑/楷/仿宋（Regular + Bold，共 8 条 GBK 字节 BaseFont）到 `arphic-uming` / `arphic-ukai` / `wqy-zenhei` 三套 TrueType 字体的映射交给 gs，让 gs pdfwrite 在重建字体字典时能按字体名落到不同字形上，而不是全部坍缩成单一 `DroidSansFallback` 无衬线体。

**加载路径现在是两套并存**，两者都保留，各管一段场景（改动任一侧前请先读 `Dockerfile` 与 `cmd/server/pdf_normalize.go` 的注释）：

1. **主路径 —— gs 自动加载 `Resource/Init/cidfmap`（Docker 内）**：`Dockerfile` 构建期先 `cp docker-fonts/cidfmap.local /etc/ghostscript/cidfmap.local`（并按用户自备的 `simsun/simhei/simkai/simfang.ttf` 用 `sed` 替换映射目标），再 `find /usr/share/ghostscript -path "*/Resource/Init"` 把同一份文件复制成 gs 的 `Resource/Init/cidfmap`。gs 启动时会自动加载 `Resource/Init/cidfmap`，**不需要任何命令行参数**。trixie 的 gs 10.05.1 默认不存在 `cidfmap`（只有 `FAPIcidfmap`），所以直接创建即可，不涉及和发行版文件的合并冲突。构建期还有一步自检：`test -s` + `grep -cE '^/#'` 必须等于 8 条，条目数不对就让构建失败。
2. **兼容路径 —— `pdf_normalize.go::cidfmapPreambleArgs()`（仍在代码里，仍被调用）**：`tryGhostscriptRun` 每次拼 gs 命令行时都会 `append(args, cidfmapPreambleArgs()...)`。它现在**只**在 `/etc/ghostscript/cidfmap.local`（变量 `cidfmapSystemPath`）存在时返回**一个** `-I<dir>` 搜索路径参数，文件不存在（macOS 本地开发）时返回 `nil`，命令行退化为未打补丁前的形态。**历史上曾经拼的 `-c "(cidfmap.local) .runlibfile" -f` 显式加载已经删掉**——`Resource/Init/cidfmap` 自动加载后它成了重复动作。`pdf_normalize_test.go::TestCidfmapPreambleArgs` 就在锁这两个分支（不存在 → `nil`；存在 → 恰好 1 个 `-I` 参数指向 cidfmap 所在目录），改这个函数会直接把测试打红。

由于 arphic/wqy 都是**单字重字库**，gs 也不做 synthetic bold，Bold 变体只能通过"换字体制造视觉粗细差"——当前策略是宋体 Bold / 仿宋 Bold → `wqy-zenhei`（本镜像最粗的中文字体），黑体/楷体的 Bold 与 Regular 同源、视觉一致，属字库本身限制。诊断方式：`gs -dPDFDEBUG -dNOPAUSE -dBATCH -sDEVICE=pdfwrite -sOutputFile=/tmp/out.pdf <in.pdf> 2>&1 | grep -E "Substituting|CIDFSubst"`，命中 cidfmap 会看到 `Substituting font ... from /usr/share/fonts/truetype/...`；未命中才回落到 `DroidSansFallback`。新增映射条目时，GBK 字节 → PostScript name 换算关系：宋体=`cb ce cc e5`、黑体=`ba da cc e5`、楷体=`bf ac cc e5`、仿宋=`b7 c2 cb ce`，CSI 固定用 `[(GB1) 2]`；改完记得同步 `Dockerfile` 里那句"expect 8"的自检数字。

### HTTP 超时

`cmd/server/main.go` 的 `http.Server` 配置为 `ReadTimeout = WriteTimeout = IdleTimeout = 120s`。之所以放宽到 2 分钟，是因为 `/api/convert` 与 `/api/print` 在移动端场景需要：上传 10MB+ 原图 → 服务端下采样/标准化 → 回传 PDF，整条链路在 4G 网络下 15s 远远不够（[Issue #22](https://github.com/hanxi/cups-web/issues/22)）。如果未来要对个别接口设置更激进的独立超时，建议用 `http.TimeoutHandler` 包住具体子路由，而不是再调低全局值。

## 🧹 维护任务

`maintenance.go::startMaintenance` 启动一个 goroutine，每小时执行一次：

1. 读取 `retention_days`；为 `0` 时直接跳过
2. 按 `created_at < now - retentionDays` 删除 `print_jobs` 记录
3. 同步删除 `uploads/` 下的原文件与 `.print.pdf` 副文件
4. 若有删除发生：执行 `VACUUM` 回收空间 + `PRAGMA wal_checkpoint(TRUNCATE)`

管理员也可通过 `POST /api/admin/cleanup` 手动触发同一清理逻辑（`adminCleanupHandler` → `cleanupOldPrints`），前端管理页面的"立即清理"按钮即调用该接口。

## 🧩 驱动管理（单容器形态的核心机制）

**这一章是全仓最容易被后人"顺手简化"改坏的部分**，每条约定后面都写了它对应的真实故障，请连注释一起读。

相关文件：`cmd/server/driver_handlers.go`、`cmd/server/driver_registry.go`、`scripts/driver/*.sh`、`frontend/src/views/DriversView.vue`（前端「驱动」页，路由 `/drivers`，仅 admin 可见）。

### 目录约定

| 路径 | 内容 |
| --- | --- |
| `/opt/cups-drivers/scripts/install-<name>.sh` | 各驱动的安装脚本，`Dockerfile` 里 `COPY scripts/driver/install-*.sh` 进来，**构建期不执行**，由用户按需触发 |
| `/usr/local/bin/{driver-install,driver-list,driver-remove,restore-drivers}` | 四个管理命令，分别来自 `scripts/driver/` 下同名 `.sh`（`COPY` 时去掉了 `.sh` 后缀） |
| `/opt/cups-drivers/data/<driver>/manifest.txt` | 该驱动安装时产生的文件清单（容器内绝对路径，一行一个）。**它同时是"已安装"的唯一标记** |
| `/opt/cups-drivers/data/<driver>/metadata.txt` | `driver=` / `installed_at=` / `file_count=` / `arch=` |
| `/opt/cups-drivers/data/<driver>/<绝对路径镜像>` | 驱动产物文件的副本，目录结构与 manifest 里的绝对路径一一对应（`restore-drivers` 用 `"${driver_dir}${filepath}"` 拼源文件路径，所以两边**必须**保持这个约定） |
| `/opt/cups-drivers/data/custom-ppd/` | Web 上传的 `.ppd`：装到 `/usr/share/cups/model/custom/`，**写 manifest**，能被恢复 |
| `/opt/cups-drivers/data/custom-deb/packages/` | Web 上传的 `.deb`：只归档，**故意不写 manifest** |

`docker-compose.yml` 把宿主 `./.drivers` 挂到 `/opt/cups-drivers/data`。**不挂这个卷 = 每次重启丢掉所有手动安装的驱动。** `Dockerfile` 里还是 `mkdir -p /opt/cups-drivers/data` 预建了目录，这样不挂卷直接 `docker run` 时安装/列表逻辑也不会因缺目录而行为异常（数据随容器销毁，属预期）。

### 持久化原理（为什么不需要 `CAP_SYS_ADMIN`）

`driver-install.sh` 的流程是**纯文件系统 diff**，不涉及任何 overlay/mount 魔法：

1. 安装前对 `MONITORED_DIRS` 做 `find -type f` 快照（`/usr/lib/cups`、`/usr/share/cups`、`/usr/share/ppd`、`/lib/firmware`、`/usr/share/foomatic`，外加探测到的 multiarch 目录 `/usr/lib/<triplet>`），同时 `dpkg --get-selections` 记录包状态
2. 跑 `install-<name>.sh`（`export CUPS_AIO=1`）
3. 安装后再快照，`comm -13` 求出新增文件；再把"本次新装的 dpkg 包"（`dpkg -L` 展开）里**通过白名单的**文件也并进来
4. 白名单过滤 + 去重 → 逐个 `cp -a` 到 `/opt/cups-drivers/data/<driver>/<绝对路径>` → 写 `manifest.txt` + `metadata.txt` → `ldconfig`
5. 容器重启时 `entrypoint.sh` 第一步跑 `restore-drivers`，逐行读 manifest，`mkdir -p` 父目录后 `cp -a` 回系统路径，最后 `ldconfig`

### ⚠️ manifest 白名单：为什么必须存在，且三处都要有

老实现对 dpkg 来源的文件**完全没过滤**——AIO 模式下编译型驱动会现场 `apt-get install build-essential`，于是 `gcc` / `binutils` / `libc6-dev` 全被算作"新装包"，`/usr/bin/gcc`、`/usr/share/man/**`、`/etc/**` 一股脑写进 manifest。而 `driver-remove` 是**按 manifest 逐条 `rm -f`** 的：**卸载一次驱动就把系统 gcc/binutils 和一堆系统库删了，容器直接残废。** `restore-drivers` 同理会用几个月前的旧二进制 `cp -a` 覆盖系统当前文件。

现在 `driver-install.sh` / `driver-remove.sh` / `restore-drivers.sh` **三个脚本各有一份同样的白名单**（函数名分别是 `_is_monitored_path` / `_is_removable_path` / `_is_restorable_path`），规则完全一致：

**ALLOW（必须落在其中之一）**：`/usr/lib/cups`、`/usr/share/cups`、`/usr/share/ppd`、`/usr/share/foomatic`、`/lib/firmware`、`/usr/lib/firmware`，以及探测到的 `/usr/lib/<multiarch-triplet>`（闭源驱动的 `.so` 会装在这里）。

**DENY（即使落在 ALLOW 内也一律排除）**：
`/usr/bin/*`、`/usr/sbin/*`、`/bin/*`、`/sbin/*`、`/usr/local/bin/*`、`/usr/local/sbin/*`、`/etc/*`、`/var/*`、`/usr/include/*`、`/opt/cups-drivers/*`、`/tmp/*`、`/usr/share/{doc,man,locale,info}/*`、`/usr/share/cups/doc-root/*`（CUPS 自带 Web UI 静态资源，不是驱动产物）、`*/pkgconfig/*`、`*.a`、`*.o`、`*.la`。

> 驱动真正需要的可执行文件都在 `/usr/lib/cups/filter/` 与 `/usr/lib/cups/backend/`，**绝不会**出现在 `/usr/bin` 或 `/usr/sbin`——所以排除这些目录是安全的。

**🚫 不要因为"install 侧已经过滤了"就删掉 remove / restore 侧的守卫。** 跑过旧版本的用户手上已经存在**被污染的 `.drivers` 快照**，那些老 manifest 里就躺着 `/usr/bin/gcc`。remove/restore 两侧的守卫是给这批存量快照兜底的，必须**永久保留**；命中时的行为是**只告警并跳过，绝不 `rm` / 绝不 `cp`**，并在结尾汇总 `skipped_count`。

### ⚠️ AIO 编译脚本的「单一 EXIT trap」约定

bash 对同一信号**只保留最后一次注册的 handler**。老实现里编译型脚本注册了两个 `trap ... EXIT`（一个卸载 AIO 编译依赖、一个删临时构建目录），后注册的直接把前一个覆盖掉，两种翻车都真实发生过：

- `install-canon-capt.sh` / `install-foo2zjs-firmware.sh`：**AIO 清理 trap 被覆盖** → `build-essential` / `gcc` 永不卸载 → 被 `driver-install` 当成"新装包"，整条工具链的文件（在加白名单之前）被写进 manifest → 卸载驱动时删掉系统 gcc
- `install-escpr2.sh`：**删临时目录的 trap 被覆盖** → 几十 MB 的构建目录泄漏在容器 `/tmp`

现在这三个脚本统一成**全局唯一一个** `trap _cleanup EXIT`，`_cleanup()` 内部按分支做所有清理（`rm -rf "${BUILD_DIR}"` + `_AIO_DEPS_INSTALLED=1` 时 `apt-get purge -y --auto-remove ${BUILD_DEPS}`），并且 `local rc=$?` / `return $rc` 保住原退出码。**新增编译型驱动脚本时必须遵守这条约定**：一个脚本只允许一个 EXIT trap，所有清理写进那个函数里。

另一条相关约定：`_cleanup` 里 AIO 模式下**只 `apt-get clean`，绝不 `rm -rf /var/lib/apt/lists/*`**。运行中的容器清掉 apt 索引后，紧接着装第二个驱动就会因为没有包索引而 `apt-get install` 失败（"连续装两个驱动直接翻车"）。各 `install-*.sh` 末尾清索引的语句也统一加了 `if [ "${CUPS_AIO:-0}" != "1" ]` 守卫——只有构建期才为省体积清。

### 退出码约定

`install-*.sh` 共同遵守（`driver-install.sh` 里以注释形式写死）：

| 退出码 | 含义 | `driver-install` 的行为 |
| --- | --- | --- |
| `0` | 安装成功 | 继续做 diff / 写 manifest |
| `3` | **当前 CPU 架构不支持该驱动**（厂商未提供该架构二进制） | 打印中文说明、`discard_driver_data`（删掉可能已创建的空数据目录）、**不写 manifest**、以 3 退出 |
| 其他非零 | 真正的失败（下载 / 编译 / dpkg 失败） | 同样 `discard_driver_data` 后原样透传退出码 |

为什么必须区分：老实现里"架构不支持"分支是 `exit 0`，`driver-install` 照常写 `manifest.txt`，Web UI 于是显示**「已安装」**，用户以为驱动可用。当前 `exit 3` 的脚本：`install-gutenprint.sh`（armhf / armel）、`install-canon-ufr2.sh`（非 amd64/arm64）、`install-epson-cn.sh`（非 amd64）、`install-konica-bizhub.sh`（非 amd64/arm64）。

还有一条同源约定：**退出码 0 但一个新文件都没产生，也视为失败。** `driver-install.sh` 在 diff+过滤后如果 `new-files.txt` 为空，会打印明确错误、`discard_driver_data`、`exit 1`，**拒绝写 manifest.txt**——否则又会出现"UI 显示已安装、实际什么都没装"。

### 架构探测约定

runtime 镜像**没有 `dpkg-dev`**，所以：

- **不能用 `dpkg-architecture`**。老代码用它取架构和 multiarch triplet，在 arm 上命令直接不存在 → 静默回落到硬编码的 `x86_64-linux-gnu`，导致监控目录是个不存在的路径，闭源驱动的 `.so` 变更**完全抓不到**；`driver-list.sh` 那边则回落到 `uname -m` 的 `aarch64`，和 `metadata.txt` 里 `arch=arm64` 永远不相等，于是每个已装驱动都被误报"架构不一致"。
- 统一用 **`dpkg --print-architecture`**（`dpkg` 本体一定在）拿 Debian 架构名 `amd64` / `arm64` / `armhf`。`driver-install.sh::detect_deb_arch` 在连 `dpkg` 都没有时才退到 `uname -m`，且只作诊断展示。
- multiarch 库目录用 `detect_multiarch_libdir()`：`dpkg-architecture` 存在就用（构建期/开发机）→ 否则 glob `/usr/lib/*-linux-gnu*` → 都拿不到就**返回空串，调用方跳过该目录**（绝不使用猜错的路径）。
- Go 侧 `driver_registry.go::currentDebArch()` 把 `GOARCH` 映射到**同一套 Debian 命名**（`amd64`→`amd64`、`arm64`→`arm64`、`arm`→`armhf`、`386`→`i386`，未知架构原样返回），这样 `DriverMeta.Arch`（写的是 `amd64`/`arm64`/`armhf`/`all`）、`metadata.txt` 的 `arch=`、脚本里的判断三方才能直接比较。二进制是 `CGO_ENABLED=0` 交叉编译的，`GOARCH` 就是运行架构。

### 上传自定义驱动

- **`.ppd`**：校验首 256 字节含 `*PPD-Adobe` → 写 `/usr/share/cups/model/custom/<name>.ppd` → 在 `custom-ppd/` 下存一份同结构副本 → **追加 manifest**（`appendManifestLine` 幂等去重）+ 写 `metadata.txt`。**能被 `restore-drivers` 恢复。**
- **`.deb`**：`dpkg -i` 失败时 `apt-get install -y -f --no-install-recommends` 补依赖，**然后必须再 `dpkg -i` 一次**（老实现修完依赖就返回，等于白跑一趟 apt）。成功后只把原件归档到 `custom-deb/packages/`，**故意不写 `manifest.txt`**——`restore-drivers` 是按 manifest 里的绝对路径 `cp -a` 回文件系统的，对 `.deb` 毫无意义（真正的安装动作在 maintainer script 里），写了只会把 `.deb` 文件拷到荒谬的路径。因此 **`.deb` 上传不会随容器重启自动恢复，重启后需要手动重装**；`GET /api/admin/drivers` 会连同 `customDebNotice` 一起把这句话回给前端，`upload` 响应里也有 `warning` 字段。
- 🔐 **安全风险面（有意保留的管理员能力）**：上传 `.deb` 等价于**容器内 root RCE**——dpkg 会以 root 执行包里的 maintainer script（`preinst`/`postinst`…），可以做任何事。该接口受 `RequireSession` + `RequireAdmin` + `ValidateCSRF` 三重保护，且每次上传都把上传者用户名写进日志用于审计。**部署时请把管理员账号密码视作等同于容器 root 凭据。**
- 文件名一律经 `safeUploadFilename` 收敛（先手工切掉 Windows 反斜杠路径，再 `filepath.Base`，拒绝 `.`/`..`/隐藏文件/含分隔符），不依赖标准库 multipart 恰好做过 `Base`。
- ⚠️ **大小上限的正确写法**：`r.ParseMultipartForm(n)` 的 `n` 是 **maxMemory（内存缓冲上限）而不是请求体上限**——超出部分 Go 会静默 spool 到临时文件，所以单靠它**拦不住**超大上传（本接口原先写 `ParseMultipartForm(50 << 20)` + 注释 `// 50 MB limit`，就是对 Go 语义的误解）。真正的硬上限必须 `r.Body = http.MaxBytesReader(w, r.Body, driverUploadMaxBytes)` 包一层，之后 `ParseMultipartForm` 才会在超限时报错；`maxMemory` 另外给个小值（本接口 8MB）让大包落盘而不是整个进内存。
  > 📌 遗留待办：`print_handlers.go` / `convert_handler.go` / `estimate_handler.go` / `compose_handler.go` 目前都是 `ParseMultipartForm(512 << 20)`，同样把 maxMemory 当成了上限——含义是「允许把最多 512MB 塞进内存」且**没有任何请求体硬上限**。这几处属于本次改动之外的历史代码，未一并修改；后续收敛时同样应改成 `MaxBytesReader` + 小 maxMemory。

### `lpinfo` 检测：格式假设与型号解析优先级

`GET /api/admin/drivers/detect` 用的是 **`lpinfo -l -v` 长格式**。老代码调的是短格式 `lpinfo -v`（每行只有 `<class> <uri>` 两列，**根本没有厂商型号**）却按长格式去解析引号里的 make-and-model，后果是连锁的：网络打印机型号恒为空 → `checkHasDriver(" ")` 因 `strings.Contains(desc, " ")` 恒为 `true` → `findBestPPD("")` 返回 `lpinfo -m` 的**第一条 PPD**，给打印机套上一个完全无关的驱动。

现在的实现（`parseLpinfoDevices` / `buildDetectedPrinter`）：

- 以 `Device:` 开块，块内每行按第一个 `=` 拆 key/value，未知 key 忽略——刻意宽容，某版本改了字段顺序或缩进也不会整体解析失败
- **过滤裸 backend 行**：`lpinfo` 还会输出 backend 自身（`network socket` / `direct hp` / `ipp` / `lpd` / `beh` / `dnssd`…），这类行第二列不是完整 URI（**不含 `://`**），必须丢掉，否则会凭空多出 5~6 台"假打印机"；同时跳过 `cups-pdf` / `cups-brf` / `file:///dev/null` 等虚拟设备
- **型号解析优先级（按可信度）**：`make-and-model` → `device-id` 的 `MFG`/`MDL`（含 `MANUFACTURER`/`MODEL` 长写法，并去掉型号里重复的厂商前缀）→ `info` → URI 路径（仅 `usb://厂商/型号` 能解析出来）。`splitMakeAndModel` 把 CUPS 填的 `Unknown` 等价于空；只有一个词时当型号处理
- **空型号短路**：`findBestPPDFromModels` 里 `len(model) < 2` 直接返回空串（连"单字符解析残渣"一起挡掉）。挑不到 PPD 时 `setup` 就**不传 `-m`**，让 CUPS 走 driverless / IPP Everywhere——这是安全降级，比硬套一个无关 PPD 好得多
- `checkHasDriver` 直接复用 `findBestPPDFromModels`：「能挑出 PPD」就是「已就绪」的定义，两处口径一致才不会出现"UI 显示已就绪、`lpadmin` 却挑不出 PPD"的割裂
- `lpinfo -m` 输出可能几千行，整个检测过程**只取一次**（`listCUPSModels`），不为每台设备 fork 一个进程

### 一键设置（`/drivers/setup`）的步骤

请求字段名以 `/detect` 的响应为准：`{deviceUri, driverName?, manufacturer?, model?}`（历史前端发的 `{uri, driverMatch, installDriver}` 与后端完全不匹配，这条主路径曾经 100% 返回 400）。任务内部依次：驱动未装则 `driver-install`（以 `manifest.txt` 存在与否判断）→ 确定厂商/型号（URI 解析不出来时用请求里带来的 `manufacturer`/`model` 补全）→ `findBestPPD` → `sanitizePrinterName` 生成队列名 → `lpadmin -p <name> -E -v <uri> [-m <ppd>]` → `lpadmin -p <name> -o media=iso_a4_210x297mm`（国内默认 A4，失败只告警不影响整体成功）。

## 🚀 容器启动流程（`entrypoint.sh`）

单容器形态下 `entrypoint.sh` 是 `ENTRYPOINT`，顺序如下（编号与文件中的注释块一致）：

1. **`restore-drivers`**：恢复 `.drivers` 快照里的驱动文件
2. **CUPS 管理员用户**：`/etc/shadow` 里没有 `$CUPSADMIN` 时 `useradd -r -G lpadmin` + `chpasswd`，并按 `$TZ` 配 tzdata
3. **CUPS 配置还原**：`/etc/cups/cupsd.conf` 不存在（挂了空卷）时从镜像内的 `/etc/cups-bak/` 复制
4. **HP 1020 PPD 的 Letter → A4 一次性修补**（issue #48）：只改 `*Product` + `*FoomaticIDs` 双重指纹命中、且当前默认仍是 Letter、且 `*PageSize A4` 存在的存量 PPD，改前备份 `.bak-cupsweb-issue48`
5. **HP host-based 固件上传**：容器内没有 udev daemon，手动喂 `SUBSYSTEM=usb` 调用 foo2zjs 上游的 `/usr/lib/udev/hplj{1000,1005,1018,1020}` + `hpljP{1005,1006,1505}`，**后台跑**（上游脚本里有 `sleep 3`，同步调用会拖慢 cupsd 启动），日志在 `/var/log/cups/hp-firmware.log`
6. **dbus + avahi + ipp-usb**：后台拉起，用于 driverless / IPP Everywhere 发现；三者均允许缺失/失败，不影响 cupsd
7. **cupsd + watchdog**（见下方专门说明）
8. **等 cupsd 就绪**：`lpstat -r` 轮询，最多 30 次 × 1s
9. **AirPrint A4 `media-ready` 修补**（issue #82）：后台对命中的 HP 1020 队列执行 `lpadmin -p NAME -o media=iso_a4_210x297mm`。iOS 打印面板的纸张候选读的是 `media-ready`/`media` 而不是 `media-default`，所以第 4 步只改 PPD 默认值还不够
10. **`exec /cups-web`**：作为 PID 1 前台运行（`exec` 替换父进程不会杀掉已 fork 的后台子 shell）

### ⚠️ cupsd 必须在 watchdog 子 shell **内部前台**启动

bash 的 `wait` 只能等待**当前 shell 自己的子进程**。老实现在主 shell 里 `cupsd -f &` 拿 PID，再在 watchdog 子 shell 里 `wait $CUPSD_PID`——那个 PID 对子 shell 来说是**兄弟进程**，bash 立刻返回 **127**（not a child of this shell）而不阻塞（老代码还用 `|| true` 把这个错误吞了）。于是循环秒进下一轮 → `sleep 2` → 又 fork 一个 cupsd → 631 端口已被占用、新进程秒退 → **每 2 秒一次重启风暴，日志刷满**。

正确形态（当前实现）：`/usr/sbin/cupsd -f` 写在 `( while true; … ) &` 子 shell **里面**、以前台方式跑。这样它是子 shell 的直接子进程，子 shell 会阻塞到 cupsd 真正退出，`$?` 也是 cupsd 的真实退出码；整个子 shell 再 `&` 到后台，不阻塞后续启动步骤。

**🚫 不要把 `/usr/sbin/cupsd -f` 挪到子 shell 外面再配 `wait`** —— 那就是上面那个 127 死循环。

**fast-fail 退避**：`CUPSD_MIN_UPTIME=5`、`CUPSD_MAX_FAST_FAILS=5`。存活 < 5s 记一次"短命退出"，连续 5 次就打印醒目中文错误（提示大概率是 `cupsd.conf` 语法错或 631 端口被占用）并 `break` 彻底放弃重启；只要有一次存活超过 5s（说明是偶发崩溃而非配置问题）计数器清零。

### ⚠️ `restore-drivers` 必须永远 `exit 0`

`entrypoint.sh` 是 `set -e`，而 `restore-drivers` 是它的**第一步**。驱动恢复是"尽力而为"的操作：快照可能被旧版本写坏、目标路径可能被别的包占成目录、挂载可能只读。这些**都不该阻塞启动**——一旦容器起不来，用户连 Web UI 都进不去，**没法自救卸载那个坏驱动**。

所以是双层保险：`restore-drivers.sh` 本身**故意不用 `set -e`**（只 `set -uo pipefail`），逐文件记账、结尾汇总 `total_errors` / `total_skipped` 并打印"驱动恢复不完整，但不阻塞容器启动；可在 Web UI 里卸载后重新安装该驱动"，最后**无条件 `exit 0`**；`entrypoint.sh` 那一行再补一层 `|| echo "[entrypoint] WARN: restore-drivers 部分失败，继续启动"` 兜底，防它因意外信号/非零退出把 `set -e` 的 entrypoint 带崩。

## 🔧 开发环境

### 本地搭建

```bash
# 1. 前端
cd frontend
bun install
bun run dev       # 开发模式（Vite，默认 :5173，代理 /api → :8090）

# 2. 后端
cd ..
go mod download
go build -o bin/cups-web ./cmd/server
./bin/cups-web    # 默认监听 :8080，数据库 ./data/cups-web.db
```

### 使用 Makefile

```bash
make all            # 构建前端 dist + Go 二进制
make frontend       # 仅构建前端
make build          # 仅构建 Go 二进制
make docker-build   # 构建单容器（AIO）镜像 cups-web:latest（合并后只有一份 Dockerfile）
make clean          # 删除 bin/cups-web
```

> **前后端整合规则**：Go 使用 `//go:embed dist/**` 将前端产物嵌入二进制，因此 **必须先构建前端** 再构建后端（CI 与 `Makefile all` 已按此顺序执行）。

> **构建规范**：编译后端**必须**使用 `make build`（或等效的 `go build -ldflags='...' -o bin/cups-web ./cmd/server`），**禁止**裸执行 `go build ./cmd/server` —— 后者会在项目根目录生成名为 `server` 的垃圾文件（Go 默认用包目录名作为输出文件名），而非正确的 `bin/cups-web`。如果只需做语法/类型检查而不生成二进制，使用 `go vet ./cmd/server`。

### Vite 开发代理

`frontend/vite.config.js` 里配置了 `/api → http://localhost:8090` 代理，本地调试建议：

```bash
# 后端启动在 8090
LISTEN_ADDR=:8090 go run ./cmd/server

# 前端启动在 5173
cd frontend && bun run dev
```

### 构建产物分包

Vite 已配置 `manualChunks`：

- `vue-vendor`：vue / vue-router
- `ui-vendor`：`@nuxt/ui` / `reka-ui` / `@vueuse`
- `pdf-vendor`：`pdfjs-dist`（仅预览，PDF 生成已迁移到后端）

## 🚢 部署

### Docker 多阶段构建

`Dockerfile`（**单容器 AIO：CUPS + Web 同镜像**）有**五个**构建阶段，**全部覆盖 `linux/amd64` + `linux/arm64` + `linux/arm/v7` 三架构**：

1. `frontend-build`（`node:20-slim` + `npm`）：`npm ci` + `npm run build` 出 Vite dist
2. `java-builder`（`--platform=$BUILDPLATFORM debian:trixie-slim` + apt `openjdk-21-jdk-headless` + Apache Maven tarball）：构建 `ofd-converter.jar`
3. `builder`（`golang:1.26`）：`go build` 输出二进制（`CGO_ENABLED=0`，`-ldflags -X main.Version=$VERSION`）
4. `cups-builder`（`debian:trixie-slim`）：跑 `scripts/build/install-cups.sh` 从 OpenPrinting/cups 源码编译（`CUPS_VERSION=2.4.19`，`--prefix=/usr` + `--libdir=/usr/lib/<multiarch>`），再把编译产物 `tar` 打包成 `/tmp/cups-compiled.tar`
5. `runtime`（`debian:trixie-slim`）：装齐 CUPS 生态（`cups-filters`/`cups-daemon`/`printer-driver-*`/`hplip`/`avahi-daemon`/`ipp-usb`…）+ LibreOffice（core/writer/calc/impress）+ `openjdk-21-jre` + Ghostscript + 中文字体（`fonts-noto-cjk`、`fonts-wqy-zenhei`、`fonts-arphic-*`、`fonts-droid-fallback`），然后解包 overlay 覆盖 apt 版 CUPS

**运行身份是 root，不是 `nonroot`**：容器内要跑 `cupsd`、`lpadmin`、`dpkg`（运行时安装驱动），还要往 `/usr/lib/cups`、`/usr/share/ppd`、`/lib/firmware` 等系统路径写驱动文件。`docker-compose.yml` 里显式写了 `user: root`。

> 💡 **`cups-builder` 为什么要「apt 装一遍 cups 再用源码编译版覆盖」**：`cups-filters` 会把 apt 版 `cups` 当依赖拉进来，由 Debian 包负责创建 `lp`/`lpadmin` 用户组、`/etc/cups` 目录骨架、systemd unit 等**集成脚手架**；随后 `make install`（同样 `--prefix=/usr`）用上游编译产物覆盖 `cupsd` / `libcups.so.2` / `cups-client` 等文件。这样既保留 Debian 侧的脚手架，又拿到 OpenPrinting 上游的最新版本，而 `libcups2` ABI 兼容让 `cups-filters` 和所有 `printer-driver-*` 继续可用。runtime 阶段的 overlay 就是 `tar xf /tmp/cups-compiled.tar -C / && ldconfig`；tar 的文件清单里 `libcups*` 路径用 `dpkg-architecture -qDEB_HOST_MULTIARCH`（**构建阶段装了 `dpkg-dev`，这里可以用**；运行时脚本里不行，见「驱动管理 → 架构探测约定」）。

> 🚨 **`cups-builder` 阶段的 `ca-certificates` 请勿删除**：`scripts/build/install-cups.sh` 用 `wget` 从 GitHub Releases 下载 CUPS tarball，没有 CA 根证书时 wget 无法校验 TLS，**以退出码 5（SSL verification failure）失败**，而脚本是 `set -euo pipefail`，整个构建当场崩掉（CI 报 `install-cups.sh ... exit code: 5`）。`debian:trixie-slim` 默认不带 `ca-certificates`；旧的单阶段 `cups/Dockerfile` 因为运行时依赖里已经包含它才没暴露这个坑，拆成独立 builder 阶段后**必须显式声明**。这是真实修过的 CI 打包失败根因。

> 💡 **`HOME` / LibreOffice profile**：runtime 阶段显式 `ENV HOME=/root` + `XDG_CACHE_HOME` + `DCONF_USER_CONFIG_DIR` 并预建目录。原因有两条：①LibreOffice headless 必须有可写 HOME 来落 user profile，拿不到就**静默退出**、`--convert-to pdf` 返回 0 却不产出 PDF；②Docker 只在 `USER` 指向 `/etc/passwd` 里的用户时才隐式给 `HOME`，部署方用 k8s `securityContext.runAsUser` 或 `docker run -u` 换成任意 uid 时 `HOME` 会退化成 `/`，转换开始莫名失败。写死 ENV 后路径至少是确定的、故障可诊断。

> 💡 **关于三架构覆盖的基础镜像选型**（历史决策，`bookworm` → `trixie`、JDK 17 → 21 之后结论不变）：最初 `frontend-build` 用 `oven/bun`、`java-builder` 用 `maven:3.9-eclipse-temurin-17`，但这两个基础镜像都不支持 32-bit ARM：
> - `oven/bun`：Bun 官方明确不支持 32-bit ARM（[oven-sh/bun#5060](https://github.com/oven-sh/bun/issues/5060) "Closed as not planned"，仅 arm64/x64）。**替代方案**：切到 `node:20-slim`（官方 manifest 覆盖 `amd64`/`arm32v7`/`arm64v8`），用 `npm ci` + `npm run build` 替换 `bun install` + `bun run build`；前端 `package.json` 里 scripts 全是标准 Vite/Node 命令，不依赖 bun 专有 API，迁移无业务代码改动。代价是必须维护 `frontend/package-lock.json`（和 `bun.lock` 并存；`npm ci` 要求 lockfile 与 `package.json` 严格一致，开发时如果用 `bun add` / `bun remove` 改了依赖，需同步跑一次 `npm install` 更新 `package-lock.json` 再提交，否则 CI 会在 `npm ci` 阶段挂掉）。
> - `maven:3.9-eclipse-temurin-17`：Eclipse Temurin 对 "Linux ARM 32-bit Hard-Float" 仅 JDK 8/11 有二进制，JDK 17/21/25 [官方明确 Not Supported](https://adoptium.net/supported-platforms)；Maven 官方镜像同样没有 armhf manifest。**现用方案**：`FROM --platform=$BUILDPLATFORM debian:trixie-slim AS java-builder`，把 java-builder 阶段**固定跑在 host 本地架构**（GitHub Actions 上永远是 amd64），apt 装 `openjdk-21-jdk-headless`，Maven 用 Apache 官方 tarball（`MAVEN_VERSION=3.9.9`）；产物 `ofd-converter.jar` 是纯 Java 字节码（`maven.compiler.source=1.8`），在 runtime 阶段被各架构的 JRE 直接 `COPY --from=java-builder` 过来复用，跨架构通吃。**为什么必须锁 `BUILDPLATFORM`**：QEMU 用户态模拟 armhf 下现代 OpenJDK 不稳定——Maven 无论是用 Debian 的 `apt install maven` 还是用 Apache 官方 tarball 启动都会随机抛 `java.lang.ClassNotFoundException: org.apache.maven.cli.MavenCli`，堆栈完全一致（只差 classworlds 版本行号：Debian 包版的 `SelfFirstStrategy.java:50` vs tarball 版的 `:42`），说明问题在 JVM 层（QEMU 下的 ClassLoader / JIT 稳定性），不是 Maven 安装方式能救的；Adoptium 官方放弃 JDK 17+ armhf 二进制也印证了"ARM 32-bit 上的现代 JVM 本来就是薄弱环节"。让 java-builder 锁 amd64 就彻底绕开了这堵墙，也是 Docker 官方推荐的 multi-arch Java 最佳实践（纯字节码跨架构是 JVM 的第一性原理）。**为什么顶部需要 `# syntax=docker/dockerfile:1`**：`BUILDPLATFORM` 是 BuildKit 前端注入的自动变量，旧 buildx 环境若缺失该声明会静默把它当成空，`--platform=$BUILDPLATFORM` 退化成默认 target，java-builder 又会落回 QEMU。**为什么 `FROM debian:trixie-slim AS runtime`（以及 `cups-builder`）不加 `--platform`**：runtime 阶段要装 LibreOffice/JRE/中文字体/打印驱动并真正被各架构的 Docker 节点拉取运行，`cups-builder` 编出的是**架构相关的原生二进制**，两者都必须跟随 `TARGETPLATFORM` 生成三份；锁 amd64 会让 arm64/armhf 节点拉到 amd64 层、QEMU 模拟整个 runtime，完全跑偏。**Maven 为什么仍用 tarball 而不是 `apt install maven`**：虽然 host amd64 上 `apt install maven` 不会触发 QEMU 坑，但 Debian 包依赖 `dpkg triggers + update-alternatives` 更新软链（[carlossg/docker-maven#213](https://github.com/carlossg/docker-maven/issues/213)），换 base 镜像或升级系统时偶有兼容性问题；Apache tarball 的 `lib/` 自包含所有 jar，不依赖任何 OS 打包细节，一劳永逸。tarball URL 走 dlcdn.apache.org → archive.apache.org 的 fallback 链（前者只保留 current release，后者永久归档），升级 Maven 时只需改 `Dockerfile` 里的 `MAVEN_VERSION`。

### docker-compose

`docker-compose.yml` 现在只有**一个** `cups` 服务（原来是 `cups` + `web` 两个），`image: hanxi/cups-web:latest`，端口 `631:631`（CUPS）+ `1180:8080`（Web）。关键配置及其理由：

| 配置 | 为什么 |
| --- | --- |
| `user: root` | 要跑 cupsd / lpadmin / dpkg，还要往系统路径写驱动文件 |
| `security_opt: [apparmor:unconfined]` | issue #91：PVE LXC 等环境下 `apparmor="DENIED" … comm="jobs.cgi"` 会导致打印失败；合并单容器后它同时也保护 LibreOffice / OFD 转换子进程 |
| `./.etc:/etc/cups`、`./.data:/data`、`./.uploads:/uploads` | CUPS 配置 / 数据库 / 上传文件持久化 |
| **`./.drivers:/opt/cups-drivers/data`** | **驱动快照持久化**。删掉这个卷 = 重启后丢失所有手动安装的第三方驱动，需要在 Web「驱动」页面重装一遍（见「驱动管理」章节） |
| `/dev/bus/usb:/dev/bus/usb` + `device_cgroup_rules: ['c 189:* rmw']` | issue #81：USB 打印机热插拔。`devices:` 是启动时一次性绑定，打印机"后开机"时宿主 udev 新建的节点不会传播进容器；改成目录 bind-mount 才能实时反映新节点 |
| `/run/udev:/run/udev:ro` | 让 libusb 读到设备属性，改善识别（宿主无 `/run/udev` 时该挂载可删） |

### CI/CD

两条 workflow：

- **`build-release.yml`**：push 到任何分支和 tag 时，针对 7 个平台交叉编译二进制（`linux/amd64`、`linux/arm64`、`linux/armv7`、`linux/loong64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64`），tag push 时自动创建 Release。CI 使用的 Go 版本（`setup-go` 的 `go-version`）与 `go.mod` 保持一致（当前 `1.26`），升级 `go.mod` 时请同步 CI。
- **`docker-publish.yml`**：push 到 `master` 或 `v*` tag 时构建并推送镜像。**合并单容器后这里只剩一个 `build` job**（原来是 cups 镜像 + cups-web 镜像两个 job），单份 `Dockerfile` 出 `linux/amd64,linux/arm64,linux/arm/v7` 三架构的 `hanxi/cups-web`，`VERSION=${{ github.ref_name }}` 作为 build-arg 注入版本号，缓存 scope 为 `cups-web`。开头还有一步 `Free disk space`（删 dotnet/android/CodeQL 缓存）——AIO 镜像把 CUPS 编译 + LibreOffice + 驱动生态塞进一份镜像后体积很大，GitHub runner 默认磁盘不够。

> 💡 补充说明：
> - `linux/armv7` 使用 `GOARCH=arm` + `GOARM=7`，覆盖树莓派 2/3、主流 ARM SBC 等 32 位硬浮点设备；matrix 里通过 `goarm` 字段声明，Build 步骤已把 `GOARM` 透传到 `env`（其他非 arm 目标此字段为空不生效）。
> - `linux/loong64` 依赖 `modernc.org/sqlite` ≥ `v1.34`（`v1.29.0` 尚未支持 loong64 架构）。
> - 由于全仓严格 `CGO_ENABLED=0`，新增其他 modernc 已支持的架构（`riscv64` / `s390x` / `ppc64le` 等）只需往 `build-release.yml` 的 matrix 里加一行 `goos/goarch/suffix`，无需额外工具链。

### 版本管理

使用 `bump-version.sh` 打 tag：

```bash
./bump-version.sh patch    # 默认
./bump-version.sh minor
./bump-version.sh major
```

## 🎯 常见开发任务

### 新增 API 接口

1. 在 `cmd/server/` 下新建 `xxx_handler.go`，导出 handler 函数
2. 在 `main.go` 对应的 subrouter（`api` / `protected` / `admin`）中注册路由
3. 前端在 `frontend/src/utils/api.js` 中新增调用方法，并在视图中使用
4. 若是写接口，确认前端 `fetch` 会带上 `X-CSRF-Token` 头

### 修改数据库结构

1. 在 `internal/store/` 中修改或新增模型
2. 在 `store.go::migrate()` 中：
   - 新表：追加 `CREATE TABLE IF NOT EXISTS ...`
   - 旧表加字段：用 `addColumnIfMissing(ctx, db, "<table>", "<column_def>")`
3. 更新对应的 CRUD 函数
4. 本地用 `sqlite3 data/cups-web.db` 验证迁移在新库与老库上都能跑通

### 新增前端页面

1. 在 `frontend/src/views/` 新建 `.vue`，使用 Composition API
2. 在 `frontend/src/router/index.js` 添加路由；若需鉴权用 `meta: { requiresAuth: true }`，管理员页加 `requiresAdmin: true`
3. 在 `App.vue` 顶栏中按需加入导航入口（当前实现对 `admin` 角色显示「打印 / 驱动 / 管理」三个入口，桌面端是分段切换、移动端进汉堡菜单）

### 新增支持的文件类型

1. 在 `file_utils.go::detectFileKind` 加入新的 `fileKind`
2. 实现转换函数（放 `convert_utils.go` 或 `pdf_utils.go`）
3. 在 `print_handlers.go` 的 `switch kind` 中处理新类型
4. 同步更新 `estimateHandler` / `convertHandler` 中的分支（`convertHandler` 需覆盖单文件 `file` 与多文件 `files` 两种入口）

### 新增支持的打印机驱动

细节与踩坑理由见「驱动管理」章节，步骤如下：

1. **写安装脚本**：`scripts/driver/install-<name>.sh`。文件名里的 `<name>` 就是驱动的 canonical name，`Dockerfile` 的 `COPY scripts/driver/install-*.sh /opt/cups-drivers/scripts/` 会自动带上，无需改 Dockerfile。
2. **遵守退出码约定**：`0` = 成功；**`3` = 当前架构不支持**（绝不能用 `exit 0` 糊过去，否则会写出 manifest、Web UI 假显示"已安装"）；其他非零 = 真失败。架构判断一律用 `dpkg --print-architecture`（**不要用 `dpkg-architecture`**，runtime 镜像没有 `dpkg-dev`）。
3. **遵守单一 EXIT trap 约定**（只有需要现场编译 / 装编译依赖时才涉及）：整个脚本**只允许一个** `trap _cleanup EXIT`，临时目录清理和 `apt-get purge -y --auto-remove ${BUILD_DEPS}` 都写进 `_cleanup()` 的分支里；用 `CUPS_AIO` 环境变量（`driver-install` 会 `export CUPS_AIO=1`，Go 侧 `runDriverCommand` 也在 `cmd.Env` 里加了）区分"运行时容器内安装"与"构建期安装"。**AIO 模式下不要 `rm -rf /var/lib/apt/lists/*`**，否则装下一个驱动就没有 apt 索引了。
4. **产物必须落在白名单目录内**：`/usr/lib/cups`、`/usr/share/cups`、`/usr/share/ppd`、`/usr/share/foomatic`、`/lib/firmware`、`/usr/lib/firmware`、`/usr/lib/<multiarch>`。落在别处的文件不会进 manifest，重启后不会被恢复；一个新文件都没落进来时 `driver-install` 会直接判失败。
5. **注册到 `driver_registry.go::driversRegistry`**：填 `Name`（= 脚本名里的 `<name>`）、`DisplayName`、`Description`、`Arch`（`{"all"}` 或 Debian 架构名列表，决定前端「安装」按钮是否可点）、`NeedCompile`（是否现场编译，前端据此提示耗时）、`MatchPatterns`（`(?i)` 正则，供 `/drivers/detect` 按型号推荐；纯通用驱动可留空）。
6. **下载源**：第三方驱动一律走本仓库自维护的 GitHub Releases 镜像（tag 固定为 `cups-driver`），不要直连厂商 CDN（Epson/Sharp 的官方下载站有 UA/TLS 指纹风控，CI 里 403 概率高）。失败要 fail-fast（非零退出），不要静默成功。
7. **验证**：容器内 `driver-list` 看是否出现在可用列表、`driver-install <name>` 跑通、`cat /opt/cups-drivers/data/<name>/manifest.txt` 检查清单里**没有**系统文件、`driver-remove <name>` 后系统仍然完好、重启容器确认 `restore-drivers` 能恢复。

## 🧪 调试与测试

### 后端测试

```bash
go test ./...                # 全部测试
go test -cover ./...         # 带覆盖率
go vet ./...                 # 静态检查
```

> 当前仓库主要以手工测试 + 日志为主，`test/` 目录下存放临时测试用例，不参与 CI。新增核心模块时建议补 `_test.go`。

### 前端验证

```bash
cd frontend
bun run build                # 构建检查（类型与语法）
bun run dev                  # 本地调试
```

### 数据库查看

```bash
sqlite3 data/cups-web.db
.tables
SELECT * FROM users;
SELECT id, filename, status, is_duplex, is_color, created_at FROM print_jobs ORDER BY id DESC LIMIT 20;
SELECT * FROM settings;
```

## 📐 代码风格

### Go 风格

- 遵循标准 Go 命名约定与 `gofmt`
- Handler 内部通过 `appStore.WithTx(ctx, readOnly, func(tx) error { ... })` 做事务边界
- 错误响应统一使用 `writeJSONError(w, status, msg)`，成功使用 `writeJSON(w, v)`
- 文件路径：存储到 DB 的是 `filepath.ToSlash` 后的相对路径，使用时再用 `filepath.FromSlash` + `filepath.Join(uploadDir, ...)` 还原

### Vue 风格

- 单文件组件（SFC）+ `<script setup>` Composition API
- UI 组件优先用 `@nuxt/ui`（全局前缀 `U`，见 `vite.config.js`）
- 样式使用 Tailwind utility class，深色/浅色主题跟随 Nuxt UI 的 `bg-default` / `text-muted` 等语义类
- Session 信息通过 `router/index.js` 中的 `cachedSession` 缓存，避免每次路由切换都打 `/api/session`

### Git 提交

- Commit message 使用中文，格式 `feat:` / `fix:` / `refactor:` 等前缀 + 简要描述
- **禁止**在 commit message 中添加 `Co-Authored-By` 或任何 AI 署名行

## 📚 相关资源

- [CUPS 官方文档](https://www.cups.org/documentation.html)
- [IPP 规范](https://www.pwg.org/ipp/)
- [Nuxt UI v4](https://ui.nuxt.com/)
- [Tailwind CSS v4](https://tailwindcss.com/)
- [Vue 3 文档](https://vuejs.org/)
- [ofdrw](https://github.com/ofdrw/ofdrw)

---

**维护者**：涵曦（<im.hanxi@gmail.com>）
