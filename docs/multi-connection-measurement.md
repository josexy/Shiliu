# 多连接本地测量

**本次每响应限速场景中，4 连接吞吐约为单连接的 3.9 倍；不限速场景反而更慢。多连接不保证提速。**

## 方法与结果

记录日期：2026-11-22。Windows amd64、Go 1.27.3、Intel Core i9-14900HX（24 核 / 32 逻辑处理器）。客户端与 `httptest` 服务同进程运行，无外部下载。

每场景连续下载 8 个 32 MiB 文件（合计 256 MiB），逐字节核对。应用不限速，全局连接上限 16，单任务分别使用 1 或 4 个连接。多连接每片 4 MiB，每文件另发一次 1 字节探测。受限服务对每个响应限 8 MiB/s，不限制服务端总带宽。

| 场景 | 累计传输耗时（秒） | 吞吐（MiB/s） | 活动请求峰值 | 请求总数 | 进程 CPU（秒） |
| --- | ---: | ---: | ---: | ---: | ---: |
| 单连接，服务端不限速 | 0.7570 | 338.19 | 1 | 8 | 0.0000 |
| 4 连接，服务端不限速 | 0.8887 | 288.07 | 4 | 72 | 0.1250 |
| 单连接，每响应限 8 MiB/s | 32.1108 | 7.97 | 1 | 8 | 0.3281 |
| 4 连接，每响应限 8 MiB/s | 8.2205 | 31.14 | 4 | 72 | 0.1875 |

耗时包含任务创建、探测、落盘和最终发布，排除内容对照；吞吐按累计耗时计算。CPU 为 Windows `GetProcessTimes` 的内核与用户时间差，包含客户端、服务端和内容对照，读数有量化误差：0.0000 不代表零开销，不能据此推断节省 CPU。

结果受本机负载影响，不代表真实网站、TLS、远程代理、慢盘或其他系统的表现。分片增加请求、同步落盘与元信息提交成本。

## 重现

从仓库根目录执行 Windows PowerShell 脚本，各场景使用独立进程：

```powershell
./scripts/measure-download.ps1
```

程序与输出写入 `.mygo/`，下载写入测试临时目录。也可直接运行测试：

```powershell
$previousMeasure = $env:DOWNLOADER_MEASURE
try {
    $env:DOWNLOADER_MEASURE = '1'
    go test -run '^TestLocalTransferMeasurement$' -count=1 -v ./internal/download
} finally {
    $env:DOWNLOADER_MEASURE = $previousMeasure
}
```

```bash
DOWNLOADER_MEASURE=1 go test -run '^TestLocalTransferMeasurement$' -count=1 -v ./internal/download
```

非 Windows 系统的 `cpu_seconds=-1` 表示未测量 CPU。用例见 [measurement_test.go](../internal/download/measurement_test.go)。

## 正确性与兼容

自动化测试另覆盖 Range/ETag 校验、单连接回退、短读重试、分片恢复、元信息损坏、提交失败、限速、代理、认证与取消。性能测量本身不替代这些测试。

分片数据保存在 `.part.segments`，v2 `.part.meta` 仅恢复同步落盘后原子提交的完整区间；预分配长度不代表进度。旧连续 `.part` 保持单连接续传。使用条件和降级注意事项见 [使用指南](usage.md#数据与续传)。
