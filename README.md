# allpipes

Real-time GitLab pipeline monitor for all projects within a GitLab group, supporting both terminal user interface (TUI) and web dashboard modes.

## Requirements

- Go 1.22 or higher
- GitLab personal or group access token with `read_api` scope (required for private or internal projects)

## Build

Compile the binary:

```bash
go build -o allpipes ./cmd/allpipes
```

## Configuration

Settings can be provided via command-line flags or a `.env` file in the current working directory.

### Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `GITLAB_GROUP` | `""` | GitLab group path or ID |
| `GITLAB_TOKEN` | `""` | GitLab access token with `read_api` scope |
| `GITLAB_URL` | `https://gitlab.com` | Base URL of the GitLab instance |
| `MODE` | `tui` | Interface mode (`tui` or `web`) |
| `POLL_INTERVAL` | `10s` | Polling frequency |
| `PORT` | `8080` | Port for the web server |
| `HOST` | `0.0.0.0` | Host interface for the web server |
| `INCLUDE_SUBGROUPS` | `true` | Include projects in subgroups |
| `PIPELINES_PER_PROJECT` | `3` | Number of recent pipelines to fetch per project |
| `CONCURRENCY` | `10` | Maximum concurrent API requests |

### CLI Options

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-group` | `""` | GitLab group path or ID |
| `-token` | `""` | GitLab access token |
| `-tui` | `true` | Run in TUI mode |
| `-web` | `false` | Run in Web dashboard mode |
| `-url` | `https://gitlab.com` | GitLab base URL |
| `-interval` | `10s` | Polling interval |
| `-port` | `8080` | Web dashboard port |
| `-host` | `0.0.0.0` | Web dashboard host |
| `-subgroups` | `true` | Include projects in subgroups |
| `-recent` | `3` | Recent pipelines per project |

## Usage

### TUI Mode

Run in the terminal:

```bash
./allpipes -group my-group -token glpat-xxxxxxxxxxxx
```

#### Keybindings

- `Up` / `Down` or `j` / `k`: Navigate through pipelines
- `Enter` or `o`: Open pipeline in default web browser
- `p`: Open project in default web browser
- `Tab` or `1` - `4`: Switch filters (Active, All, Failed, Success)
- `/`: Filter by text (project, branch, SHA, author)
- `r`: Force immediate refresh
- `q`: Quit

### Web Mode

Start the web server:

```bash
./allpipes -group my-group -token glpat-xxxxxxxxxxxx -web -port 8080
```

Open `http://localhost:8080` in your web browser. Real-time updates are streamed via Server-Sent Events (SSE).
