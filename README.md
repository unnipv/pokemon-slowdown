# pokemon slowdown

[![Latest release](https://img.shields.io/github/v/release/unnipv/pokemon-slowdown?style=flat-square)](https://github.com/unnipv/pokemon-slowdown/releases)
[![Downloads](https://img.shields.io/github/downloads/unnipv/pokemon-slowdown/total?style=flat-square)](https://github.com/unnipv/pokemon-slowdown/releases)
[![Stars](https://img.shields.io/github/stars/unnipv/pokemon-slowdown?style=flat-square)](https://github.com/unnipv/pokemon-slowdown/stargazers)

A terminal client (TUI) for [Pokémon Showdown](https://pokemonshowdown.com),
written in Go. It plays ladder and challenge battles — Random Battle, singles
and doubles — from the terminal, without a browser.

- Binary: `slowdown`
- Platforms: macOS, Linux, Windows (amd64 and arm64)
- Protocol: the documented Showdown websocket protocol

## Demo

A short demo of a battle from the terminal.

![pokemon-slowdown demo](docs/pokemon-slowdown.gif)

## Screenshots

From a real session, Ghostty, half-block sprite backend.

![Battle screen](docs/screenshot-battle.png)

![Lobby and format list](docs/screenshot-lobby.png)

![Inspect overlay](docs/screenshot-inspect.png)


## Install

### Homebrew

```sh
brew install unnipv/tap/pokemon-slowdown
```

### Go

```sh
go install github.com/unnipv/pokemon-slowdown/cmd/slowdown@latest
```

Requires Go 1.27 or newer.

### Release binaries

Download the archive for your platform from the
[releases page](https://github.com/unnipv/pokemon-slowdown/releases). Checksums
are published as `checksums.txt` alongside each release.

## Usage

```
slowdown                     open the lobby
slowdown rand                queue a Gen 9 Random Battle
slowdown play <format>       queue a format, e.g. play gen9ou
slowdown challenge <user>    challenge a user to your default format
slowdown spectate <battle>   watch a battle id or replay URL
slowdown teams               team management
slowdown doctor              print terminal diagnostics
slowdown doctor --sprites    render a test sprite through every backend
slowdown --version
```

| Flag | Description |
| --- | --- |
| `--debug` | write protocol events to a log file |
| `--no-sprites` | disable sprites |
| `--sprites <mode>` | `auto`, `kitty`, `iterm2`, `sixel`, `blocks`, `none` |
| `--theme <name>` | `zen`, `dark`, `light`, `gameboy`, `mono`, `contrast` |

## Signing in

Guest play requires no setup. To use a name and appear on the ladder, press `:`
and choose **Sign in to a registered account…**.

| Field | Notes |
| --- | --- |
| Username | Your Pokémon Showdown name |
| Password | Required |
| Remember | Store the password in the OS keychain |

The password is not written to the config file. When *Remember* is enabled it is
stored in the platform keychain (macOS Keychain, GNOME Keyring, Windows
Credential Manager); the config file records only the username and the choice.
If no keychain is available, you are asked again on the next run.

**Sign out** (`:` → Sign out) clears the keychain entry and the stored username.

Server behaviour, verified against the live service:

- An unregistered name is claimed by signing in with any password.
- A registered name requires its correct password; a wrong password is reported.
- A name cannot be set without a password. The server refuses a bare rename with
  `Your authentication token was invalid`.

## Controls

| Key | Action |
| --- | --- |
| `1`–`4` | choose a move |
| `s` | switch (then `1`–`6`, or arrows and enter); `i` inspects the highlighted one |
| `t` | use the current mechanic: Tera, Mega, Z-Move or Dynamax |
| `i` | inspect a Pokémon; `↑`/`↓` cycles both sides and your party |
| `l` | full battle log |
| `c` | battle chat |
| `tab` | switch between battles still in progress |
| `enter` | after a battle, queue the same format again |
| `x` | close a finished battle, removing it from `tab` |
| `:` | command palette |
| `?` | keyboard help |
| `esc` | close an overlay, or return to the lobby |
| `q` | quit; asks for confirmation during a battle |
| `ctrl+c` | quit immediately |

In doubles, moves that need a target open a target picker with numeric
shortcuts. Spread and self-targeting moves resolve without one.

Forfeiting is a command palette action, not a key, and always asks for
confirmation.

## Layouts

The layout is selected from the terminal width.

| Width | Mode | Sprites |
| --- | --- | --- |
| ≥ 100 | cinematic | yes |
| 70–99 | standard | yes |
| 46–69 | sidecar | no |
| < 46 | compact | no |

Below 32×12 the app displays a "terminal too small" message.

## Sprites

Sprites are downloaded on demand from the public Pokémon Showdown sprite server
and cached locally. Loading does not block input; the sprite box is reserved and
a placeholder is shown until the image arrives.

| Backend | Terminals |
| --- | --- |
| Kitty graphics | Ghostty, Kitty, WezTerm |
| iTerm2 inline images | iTerm2 |
| Sixel | foot, mlterm, contour |
| Half-block | any truecolour terminal |

`auto` selects Kitty graphics when the terminal supports it, and half-block
otherwise. Inside tmux, `auto` uses half-block.

`showdown doctor --sprites` renders a test sprite through every backend so you
can see which ones your terminal honours. Set `sprites.mode` to force one.

Animated GIF sprites are used in half-block mode (`sprites.animate`). In Kitty
mode a static frame is drawn.

## Configuration

`~/.config/pokemon-slowdown/config.toml`, or `$XDG_CONFIG_HOME/pokemon-slowdown/`.
Every key is optional. `NO_COLOR` is honoured.

```toml
username = ""                     # Pokémon Showdown name
remember = false                  # store the password in the OS keychain
theme = "zen"                     # zen | dark | light | gameboy | mono | contrast
default_format = "gen9randombattle"
taglines = "canon"                # canon | absurd | off
taglines_random = false
notifications = "bell"            # off | bell | osc | desktop
debug = false
no_color = false
sidecar = false

[sprites]
mode = "auto"                     # auto | kitty | iterm2 | sixel | blocks | none
animate = true
size = "medium"                   # small | medium | large
# cache_dir = "/custom/path"

[keybindings]
# override any binding
```

## Files

| Path | Contents |
| --- | --- |
| `~/.config/pokemon-slowdown/config.toml` | configuration |
| `~/.cache/pokemon-slowdown/sprites/` | downloaded sprites |
| `~/.cache/pokemon-slowdown/dex/` | species and move data |
| `~/.local/share/pokemon-slowdown/teams.json` | stored teams |
| `~/.local/state/pokemon-slowdown/logs/` | debug logs (`--debug`) |

On macOS and Windows these resolve under the platform's own config, cache and
data directories.

## Notifications

When a background battle becomes your turn, `slowdown` can ring the terminal
bell, emit an OSC 9 or OSC 777 desktop notification, or use a platform helper.
It does not notify for the battle you are already viewing. Configure with
`notifications`.

## Teams

`slowdown teams` lists stored teams. Import by pasting Showdown export text into
the palette's **Import team** command; export by reading the stored team back
out. The packed format sent to the server is handled internally.

## Troubleshooting

Run `slowdown doctor` first. It reports the terminal, `TERM`, multiplexer
detection, truecolour support, detected graphics protocols, the selected sprite
backend, and every config, cache and data path.

| Symptom | Cause |
| --- | --- |
| Sprites are coloured blocks | Half-block renderer. Try `--sprites kitty` or `--sprites iterm2`, and compare with `slowdown doctor --sprites` |
| No sprites in tmux | `auto` uses half-block under tmux, where graphics passthrough is unreliable |
| Sprites look wrong after switching terminals | The terminal's graphics protocol differs; check `slowdown doctor` |
| "Terminal too small" | Window is below 32×12 |

## Privacy and security

- Passwords are stored in the OS keychain, never in the config file.
- Debug logs contain protocol events only. Passwords, assertions, cookies and
  auth tokens are never logged.
- Text from the server — chat, usernames, room titles — has terminal control
  sequences stripped before rendering, so other users cannot inject escape
  codes into your terminal.
- No telemetry or analytics.

## Development

```sh
git clone https://github.com/unnipv/pokemon-slowdown
cd pokemon-slowdown

make build      # -> ./slowdown
make run        # build and open the lobby
make check      # gofmt, go vet, staticcheck, tests
```

`make test` requires no network. `make live` runs the opt-in suites that contact
the real server and sprite server.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the project layout and how to add a
protocol event. `docs/IMPLEMENTATION_NOTES.md` records protocol findings and
renderer behaviour.

## Licence

MIT. See [LICENSE](LICENSE). Pokémon and Pokémon character names are trademarks
of Nintendo, Creatures Inc. and GAME FREAK inc. Sprite artwork is downloaded at
runtime and is not redistributed by this project; see
[THIRD_PARTY.md](THIRD_PARTY.md).
