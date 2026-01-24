# Getting Started

This guide covers setting up your development environment for building HoloMUSH plugins.

## Prerequisites

| Requirement   | Version | Purpose                        |
| ------------- | ------- | ------------------------------ |
| Go            | 1.24+   | Build and run the server       |
| Task          | Latest  | Task runner for build commands |
| Docker        | Latest  | Required for PostgreSQL        |
| Telnet client | Any     | Connect to the server          |

### Installing Prerequisites

=== "macOS"

    ```bash
    brew install go task
    ```

=== "Linux"

    Install Go from [go.dev/doc/install](https://go.dev/doc/install) and Task from
    [taskfile.dev/installation](https://taskfile.dev/installation/).

    For Ubuntu/Debian:

    ```bash
    # Go (download from go.dev)
    wget https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
    sudo tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin

    # Task
    sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b ~/.local/bin
    ```

## Installation

### Clone the Repository

```bash
git clone https://github.com/holomush/holomush.git
cd holomush
```

### Install Development Tools

```bash
task tools
```

This installs linters, formatters, and other development dependencies.

### Build the Server

```bash
task build
```

This creates the `holomush` binary in the project root.

## Running HoloMUSH

HoloMUSH uses a two-process architecture: **Core** (game engine) and **Gateway**
(protocol servers). Both must be running for a functional server.

### Quick Start

Open three terminal windows:

**Terminal 1: Start PostgreSQL**

```bash
docker run -d --name holomush-db \
  -e POSTGRES_DB=holomush \
  -e POSTGRES_USER=holomush \
  -e POSTGRES_PASSWORD=secret \
  -p 5432:5432 \
  postgres:18-alpine
```

**Terminal 2: Start Core**

```bash
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable" \
  ./holomush core
```

**Terminal 3: Start Gateway**

```bash
./holomush gateway
```

### Development Mode

For development with human-readable logs:

```bash
# Start Core with text logging
DATABASE_URL="..." ./holomush core --log-format=text

# Start Gateway with text logging
./holomush gateway --log-format=text
```

## Connecting to the Server

### Using Telnet

=== "macOS"

    ```bash
    # macOS includes telnet, or install via Homebrew
    telnet localhost 4201
    ```

=== "Linux"

    ```bash
    # Install if needed: apt install telnet (Debian/Ubuntu)
    telnet localhost 4201
    ```

You should see:

```text
Welcome to HoloMUSH!
Use: connect <username> <password>
```

### Test Credentials

For development, a test account is available:

- **Username:** `testuser`
- **Password:** `password`

## Example Session

```text
> connect testuser password
Welcome back, TestChar!
> look
The Void
An empty expanse of nothing. This is where it all begins.
> say Hello, is anyone there?
You say, "Hello, is anyone there?"
> pose looks around curiously
TestChar looks around curiously
> quit
Goodbye!
```

## Available Commands

After connecting with `connect testuser password`:

| Command | Usage                   | Description                 |
| ------- | ----------------------- | --------------------------- |
| `look`  | `look`                  | View the current location   |
| `say`   | `say Hello everyone!`   | Speak to others in the room |
| `pose`  | `pose waves cheerfully` | Emote an action             |
| `quit`  | `quit`                  | Disconnect from the server  |

## Running Tests

```bash
# Unit tests
task test

# Tests with coverage report
task test:coverage

# Integration tests (requires Docker)
task test:integration
```

## Development Workflow

### Code Quality

```bash
# Format code
task fmt

# Run linters
task lint
```

### Task Commands Reference

| Command      | Description              |
| ------------ | ------------------------ |
| `task`       | Show all available tasks |
| `task build` | Build the binary         |
| `task dev`   | Run development server   |
| `task test`  | Run all tests            |
| `task lint`  | Run all linters          |
| `task fmt`   | Format all code          |
| `task clean` | Remove build artifacts   |

## Next Steps

- Read about the [Event System](events.md) to understand how plugins interact
- Follow the [Plugin Tutorial](plugins/tutorial.md) to build your first plugin
- See the [Plugin API Reference](plugins/api.md) for complete SDK documentation
