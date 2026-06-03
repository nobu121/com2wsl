# com2wsl

Bridge Windows COM ports to WSL pseudo-serial devices.

- **Windows**: `com2wsl server` enumerates all COM ports and exposes each over TCP.
- **WSL/Linux**: `com2wsl client` connects to the server and creates symlinks under `~/.com2wsl/` (e.g. `~/.com2wsl/COM4`).

## Typical setup (virtual serial pair + Modbus)

1. Create a COM pair on Windows (e.g. com0com): **COM3** ↔ **COM4**.
2. Attach **modbus-slave** to **COM3**.
3. Start the server on Windows:

   ```powershell
   .\com2wsl.exe server
   ```

4. Start the client in WSL:

   ```bash
   ./com2wsl client
   ```

5. Point your WSL app at the paired port:

   ```bash
   ./your-collector --port ~/.com2wsl/COM4
   ```

The server opens a COM port only when the WSL client connects to its TCP data port. The client connects only after your application opens a symlink (e.g. `~/.com2wsl/COM4`), so **COM3** can stay with modbus-slave until you actually use **COM4**.

Each COM keeps one stable PTY/symlink for the life of the client. When your app closes the port, the client tears down TCP, waits for the device to be fully released, then waits for the next open—so restarting the collector does not leave the port stuck busy.

## Releases

Pre-built binaries are published on [GitHub Releases](https://github.com/nobu121/com2wsl/releases) (not stored in this repository).

| Asset | Use on |
|-------|--------|
| `com2wsl-windows-amd64.exe` | Windows — run `server` |
| `com2wsl-linux-amd64` | WSL/Linux — run `client` |

Download a release, then rename or invoke as needed. Tag a version (`git tag v0.1.0 && git push origin v0.1.0`) to trigger the release workflow.

## Build from source

```powershell
# Windows
go build -o com2wsl.exe ./cmd/com2wsl
```

```bash
# WSL / Linux
go build -o com2wsl ./cmd/com2wsl
```

Local build outputs are gitignored; do not commit them.

## Ports

| Service | Default |
|---------|---------|
| Control API | `14500` — `GET /api/ports`, `GET /api/health` |
| Data (COM*n*) | `14500 + n` — e.g. COM4 → `14504` |

Server listens on `0.0.0.0` so WSL can reach it via the Windows host IP.

## Client: find the server

The client tries, in order:

1. `--server host:port` or env `COM2WSL_SERVER`
2. `127.0.0.1:14500` (WSL mirrored networking)
3. Windows host IP from `/etc/resolv.conf` nameserver

## Serial settings

Default **9600 8N1**. Override on the server with `--baud` (e.g. `com2wsl server --baud 115200`).

## CLI

```
com2wsl server [-d] [--bind 0.0.0.0] [--control-port 14500] [--base-port 14500] [--scan-interval 2] [--baud 9600]
com2wsl client [-d] [--server host:14500] [--link-dir ~/.com2wsl] [--sync-interval 3]
```

`-d` / `--debug` logs serial and TCP connect/disconnect events (both sides).

## API example

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

Status values: `idle` (no TCP client), `active` (relay running), `busy` (COM could not be opened, e.g. in use on Windows).
