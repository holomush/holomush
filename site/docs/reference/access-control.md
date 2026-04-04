# Access Control

HoloMUSH uses Attribute-Based Access Control (ABAC) as its security model. The
same policy engine governs everything: which commands a player can run, what an
admin can do, and what a plugin is allowed to access.

## Who It Applies To

| Principal    | What's controlled                                           | How policies are set                          |
| ------------ | ----------------------------------------------------------- | --------------------------------------------- |
| **Players**  | Commands they can use (`say`, `describe`, `create`)         | Role-based seed policies (player, builder, admin) |
| **Admins**   | Privileged commands (`boot`, `shutdown`, `wall`, aliases)   | Admin role seed policies                      |
| **Plugins**  | Event emission, world queries, storage, command execution   | Declared in the plugin's `plugin.yaml`        |

The engine is **default-deny**. If there's no policy granting access, the action
is rejected.

## How It Works

Policies are evaluated on every action. The engine receives a request with three
parts — who is asking (principal), what they want to do (action), and what they
want to do it to (resource) — and checks it against all installed policies.

**For players and admins**, command authorization uses two layers at dispatch
time. Layer 1 checks whether the character can execute the command at all
(via `command:<name>` policies). Layer 2 checks whether the character has the
class of permissions the command needs — each command declares structured
capabilities like `{action: emit, resource: stream, scope: local}`, and the
engine verifies the character could perform those actions on those resource
types. Both layers must pass before the handler runs.

**For plugins**, policies are declared in the plugin manifest and installed when
the plugin loads. They're removed when the plugin unloads.

## The Policy DSL

Policies use a Cedar-style DSL. Each policy has a `name` and a `dsl` block:

```yaml
policies:
  - name: "emit-events"
    dsl: |
      permit(principal is plugin, action in ["emit"], resource is stream) when {
        principal.plugin.name == "my-plugin"
      };
```

### Statement Structure

| Part                    | Meaning                                          |
| ----------------------- | ------------------------------------------------ |
| `principal is plugin`   | Who is asking — `plugin` for plugin policies     |
| `action in ["emit"]`    | What they want to do — a list of allowed actions |
| `resource is stream`    | What they want to do it to — the resource type   |
| `when { ... }`          | Additional conditions that must be true          |

### Operators

| Operator       | Meaning                          |
| -------------- | -------------------------------- |
| `==`           | Equals                           |
| `!=`           | Not equals                       |
| `>` `<` `>=` `<=` | Numeric comparison           |
| `&&`           | Logical AND                      |
| `\|\|`         | Logical OR                       |
| `!`            | Logical NOT                      |
| `like`         | Glob pattern match (`*` wildcard)|
| `in`           | Value is in list                 |
| `containsAll`  | Collection contains all values   |
| `containsAny`  | Collection contains any value    |

Attribute paths use dot notation: `principal.plugin.name`,
`resource.stream.name`.

### Actions and Resource Types

| Access needed          | Action       | Resource type  | Resource pattern |
| ---------------------- | ------------ | -------------- | ---------------- |
| Emit events to streams | `"emit"`     | `stream`       | `"stream:*"`     |
| Read locations         | `"read"`     | `world_object` | `"location:*"`   |
| Read characters        | `"read"`     | `world_object` | `"character:*"`  |
| Read objects           | `"read"`     | `world_object` | `"object:*"`     |
| Key-value read         | `"read"`     | `kv`           | `"kv:*"`         |
| Key-value write        | `"write"`    | `kv`           | `"kv:*"`         |
| Key-value delete       | `"delete"`   | `kv`           | `"kv:*"`         |
| Execute commands       | `"execute"`  | `command`      | `"command:*"`    |

## Player Capabilities

Commands declare capability requirements. The built-in roles grant these
capabilities:

| Capability       | Role(s)          | Commands                    |
| ---------------- | ---------------- | --------------------------- |
| `comms.page`     | player and above | `page`, `whisper`           |
| `objects.create` | builder, admin   | `create` (objects)          |
| `objects.set`    | builder, admin   | `set` (object properties)   |
| `player.alias`   | player and above | `alias`, `unalias`, `aliases` |
| `admin:boot`           | admin            | `boot`                                    |
| `admin:shutdown`       | admin            | `shutdown`                                |
| `admin:wall`           | admin            | `wall`                                    |
| `admin:alias`          | admin            | `sysalias`, `sysunsalias`, `sysaliases`   |
| `admin:password.reset` | admin            | `resetpassword`                           |
| `admin:password.set`   | admin            | `resetpassword` (explicit password)       |
| `admin:session.kick`   | admin            | `resetpassword --kick`                    |

Commands without capability requirements (like `say`, `look`, `pose`, `quit`)
are available to all authenticated players.

## For Operators

The server ships with seed policies for the built-in roles. You don't need to
write policy DSL for normal operations.

Key things to know:

- **Plugin policies are self-contained.** Plugins declare what they need in
  their manifest. Review what a plugin asks for before loading it.
- **Denied actions are logged.** If a player or plugin hits a permission
  boundary, the server logs it with the principal, action, and resource.
- **Custom roles are planned** but not yet available. Currently the role set is
  fixed: player, builder, admin.

## Further Reading

- [Writing Plugin Policies](../extending/access-control.md) — Examples from simple to complex
- **Policy DSL Grammar** — EBNF grammar and railroad diagram files live in the `reference/` directory; see [Regenerating Reference Docs](index.md#regenerating-reference-docs)
- [Architecture](../contributing/architecture.md) — How the ABAC engine fits in the system
