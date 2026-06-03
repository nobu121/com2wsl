# com2wsl

将 Windows COM 串口桥接到 WSL 伪串口设备。

- **Windows**：`com2wsl server` 枚举所有 COM 口，并通过 TCP 暴露每个端口。
- **WSL/Linux**：`com2wsl client` 连接服务端，在 `~/.com2wsl/` 下创建符号链接（例如 `~/.com2wsl/COM4`）。

## 典型场景（虚拟串口对 + Modbus）

1. 在 Windows 上创建一对 COM（例如 com0com）：**COM3** ↔ **COM4**。
2. 将 **modbus-slave** 挂到 **COM3**。
3. 在 Windows 上启动服务端：

   ```powershell
   .\com2wsl.exe server
   ```

4. 在 WSL 中启动客户端：

   ```bash
   ./com2wsl client
   ```

5. 让 WSL 应用使用配对端口：

   ```bash
   ./your-collector --port ~/.com2wsl/COM4
   ```

仅当 WSL 客户端连接到对应 COM 的 TCP 数据端口时，服务端才会打开该 COM 口。客户端也只有在你的应用打开符号链接（例如 `~/.com2wsl/COM4`）之后才会建立连接，因此在你实际使用 **COM4** 之前，**COM3** 可以一直留给 modbus-slave。

每个 COM 在客户端运行期间对应一个稳定的 PTY/符号链接。应用关闭端口后，客户端会断开 TCP、等待设备完全释放，再等待下一次打开——这样重启采集程序时不会让端口一直处于占用状态。

## 发布包

预编译二进制在 [GitHub Releases](https://github.com/nobu121/com2wsl/releases) 提供，**不会**提交到本仓库。

| 文件 | 用途 |
|------|------|
| `com2wsl-windows-amd64.exe` | Windows 上运行 `server` |
| `com2wsl-linux-amd64` | WSL/Linux 上运行 `client` |

下载 Release 后按需重命名即可。推送版本标签（`git tag v0.1.0 && git push origin v0.1.0`）会自动构建并上传 Release 资源。

## 从源码编译

```powershell
# Windows
go build -o com2wsl.exe ./cmd/com2wsl
```

```bash
# WSL / Linux
go build -o com2wsl ./cmd/com2wsl
```

本地编译产物已在 `.gitignore` 中忽略，请勿提交到仓库。

## 端口

| 服务 | 默认值 |
|------|--------|
| 控制 API | `14500` — `GET /api/ports`、`GET /api/health` |
| 数据（COM*n*） | `14500 + n` — 例如 COM4 → `14504` |

服务端监听 `0.0.0.0`，以便 WSL 通过 Windows 主机 IP 访问。

## 客户端：查找服务端

客户端按以下顺序尝试：

1. `--server host:port` 或环境变量 `COM2WSL_SERVER`
2. `127.0.0.1:14500`（WSL 镜像网络）
3. 从 `/etc/resolv.conf` 的 nameserver 解析出的 Windows 主机 IP

## 串口参数

默认 **9600 8N1**。在服务端用 `--baud` 覆盖（例如 `com2wsl server --baud 115200`）。

## 命令行

```
com2wsl server [-d] [--bind 0.0.0.0] [--control-port 14500] [--base-port 14500] [--scan-interval 2] [--baud 9600]
com2wsl client [-d] [--server host:14500] [--link-dir ~/.com2wsl] [--sync-interval 3]
```

`-d` / `--debug` 会记录串口与 TCP 的连接/断开事件（两端均支持）。

## API 示例

```bash
curl http://172.x.x.x:14500/api/ports
```

```json
{
  "ports": [
    {
      "name": "COM4",
      "number": 4,
      "data_port": 14504,
      "status": "idle"
    }
  ]
}
```

状态说明：`idle`（无 TCP 客户端）、`active`（中继运行中）、`busy`（无法打开 COM，例如在 Windows 上已被占用）。
