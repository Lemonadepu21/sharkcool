# Windows CPU 温度采集简化调研

调研日期：2026-10-26。

## 当前项目

当前链路是 Go 后台 → 每 5 秒启动 `sharkcool-sensors.exe --read` → 验证命名管道服务进程 → Windows 采集服务 → LibreHardwareMonitor → PawnIO。采集服务只启用 CPU，安装在 Program Files，以 LocalSystem 运行，普通用户通过只读管道获取读数。

本机实测：后台已经能返回 CPU 温度（本次只读观察为 65°C）。这说明现有采集组件在本机可用，但安装服务、.NET 自包含运行库和驱动使打包与维护比较复杂。

## 可选方案

| 方案 | SharkCool 所需实现 | 用户仍需准备什么 | 判断 |
| --- | --- | --- | --- |
| Windows 自带 WMI | 一条查询 | 主板固件必须暴露合适的传感器 | 无法作为通用 CPU 温度来源 |
| Core Temp 共享内存 | Go 调用 OpenFileMapping/MapViewOfFile，解析固定结构 | Core Temp 运行并开启共享内存 | 已使用 Core Temp 时，接入和部署最轻 |
| LibreHardwareMonitor HTTP | 请求本机 `/data.json`，解析 CPU 温度节点 | LHM 运行，开启本地 Web 服务及所需驱动 | 复用已有监控软件方便；全新用户增加一个常驻程序 |
| Go 直接读取现有管道 | 把 `--read` 的服务身份验证和有界读取移入 Go | 仍需现有温度服务和 PawnIO | 最适合先做的小改进，减少每次采样启动 .NET 进程 |
| 单个提权常驻采集程序 | 将采集服务改成应用管理的进程 | 首次/每次启动时提权，以及 PawnIO | 省去服务安装，但普通权限自启、UAC 和进程生命周期更麻烦 |

微软明确说明 `Win32_TemperatureProbe.CurrentReading` 当前不填充实时读数。ACPI 热区属于整机热管理模型，不能直接把任意热区读数标成 CPU Package。没有验证的读数应继续显示“—”。

Core Temp 官方提供 `CoreTempMappingObjectEx` 共享内存和结构说明，Go 可以直接读取，无需额外 .NET 桥接。实现时需校验 CPU/核心数量、结构版本、有限浮点值、华氏转换和 Distance-to-TjMax 标志，不能把温差当成实际温度。共享内存的可访问权限、发布者退出和读数新鲜度还需真机验证。

LHM HTTP 只应访问配置的本机回环地址，设置超时和响应大小上限，并明确筛选 CPU 的 Temperature 节点。它复用的是监控软件已有的底层采集能力，并没有消除驱动需求。

## 建议

先保留现有可用的硬件采集层，把 Go 每次启动 `--read` 改为直接连接现有管道，并保留服务 PID 校验、超时和读数范围限制。这个改动减少运行时开销，但不会简化首次安装，二者需要分开评估。

如果目标是让已装监控软件的用户免装 SharkCool 温度组件，可以增加可选 Core Temp 共享内存或 LHM HTTP 来源。没有这些软件的用户仍使用现有服务。当前任务只完成调研，没有替换用户已工作的温度服务，也没有安装新的温度驱动。

## 一手资料

- [Microsoft：Win32_TemperatureProbe](https://learn.microsoft.com/en-us/windows/win32/cimwin32prov/win32-temperatureprobe)
- [Microsoft：PC thermal management design guide](https://learn.microsoft.com/en-us/windows-hardware/design/device-experiences/design-guide)
- [Core Temp：共享内存与 SDK](https://www.alcpu.com/CoreTemp/developers.html)
- [LibreHardwareMonitor：HTTP 服务源码](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/blob/master/LibreHardwareMonitor.Windows.Forms/Utilities/HttpServer.cs)
- [LibreHardwareMonitor 官方仓库](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor)
