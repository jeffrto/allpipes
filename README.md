# allpipes

Watch the CI pipelines of every project in a GitLab group, live, from the terminal or a browser.

allpipes polls the latest pipelines of each project in the group, shows who triggered them, how long they ran, and alerts you when one passes or fails.

## Requirements

- Go 1.26 or newer to build
- A GitLab personal or group access token with the `read_api` scope. Public groups work without one, but anonymous requests get a much lower rate limit.

## Build

```bash
go build -o allpipes ./cmd/allpipes
```

## Usage

```bash
# Terminal UI
./allpipes -group my-group -token glpat-xxxxxxxxxxxx

# Web dashboard on http://127.0.0.1:8080
./allpipes -group my-group -token glpat-xxxxxxxxxxxx -web
```

The group can be a path (`acme/platform`), a numeric ID, or a URL copied from the browser. On a self-hosted instance, also pass `-url https://gitlab.example.com`.

If the group or token is wrong, allpipes exits right away and says why. Problems while polling (rate limits, timeouts, projects GitLab refuses to serve) show up as a banner in both interfaces.

### Terminal UI keys

| Key | Action |
| :--- | :--- |
| `↑` `↓` / `j` `k` | Move the selection |
| `PgUp` `PgDn`, `g` `G` | Jump a page, to the top or bottom |
| `Tab` / `1`–`4` | Switch view: All, Active, Failed, Passed |
| `/` | Filter by project, branch, SHA or author (`Esc` clears) |
| `Enter` / `o` | Open the pipeline in the browser |
| `p` | Open the project in the browser |
| `r` | Poll GitLab now |
| `q` | Quit |

### Web dashboard

Updates stream over Server-Sent Events. The page has the same views and filter (`/`, `1`–`4`, `r` work as shortcuts), can group pipelines by project, keeps a log of status changes, and can play a sound or show a desktop notification when a pipeline passes or fails.

The server listens on `127.0.0.1` by default. Use `-host 0.0.0.0` only on a trusted network: the dashboard has no login and shows pipelines from private projects.

## Configuration

Every flag has an environment variable, which can also live in a `.env` file in the working directory. Flags take precedence.

| Flag | Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `-group` | `GITLAB_GROUP` | | Group path, ID or URL |
| `-token` | `GITLAB_TOKEN` | | Access token with `read_api` |
| `-url` | `GITLAB_URL` | `https://gitlab.com` | GitLab base URL |
| `-tui` / `-web` | `MODE` | `tui` | Interface (`tui` or `web`) |
| `-interval` | `POLL_INTERVAL` | `10s` | Polling interval, at least `2s` (bare numbers are seconds) |
| `-subgroups` | `INCLUDE_SUBGROUPS` | `true` | Include projects in subgroups |
| `-recent` | `PIPELINES_PER_PROJECT` | `3` | Latest pipelines tracked per project |
| `-concurrency` | `CONCURRENCY` | `8` | Maximum concurrent API requests |
| `-host` | `HOST` | `127.0.0.1` | Web dashboard listen address |
| `-port` | `PORT` | `8080` | Web dashboard port |

### API usage

Projects with a pipeline that is running or finished in the last 15 minutes are polled every interval; idle projects are checked about six times less often, and `r` / Refresh polls everything. Pipeline details (author, duration) are fetched only when a pipeline changes. If GitLab answers with `429 Too Many Requests`, allpipes backs off until it recovers.
