---
title: evoke settings
parent: CLI
nav_order: 2
---

# evoke settings

Manage user settings stored in `~/.evoke/settings.json`.

```console
$ evoke settings              # show current settings
$ evoke settings set <key> <value>
$ evoke settings remove <key> <value>
```

## Keys

| Key | Description |
|:----|:------------|
| `path` | A source directory containing `.evoke` files. Multiple paths can be added. |

## Examples

Add a source path:

```console
$ evoke settings set path ~/my-characters
```

Remove a source path:

```console
$ evoke settings remove path ~/my-characters
```

Show all settings:

```console
$ evoke settings
{
  "paths": ["/Users/you/my-characters"]
}
```

## Source roots

The `generate` and `index` commands discover `.evoke` files from these roots (in order):

1. **`EVOKE_PATH`** environment variable (colon-separated directories)
2. **Configured paths** from `settings.json`
3. **Library directory** (`~/.evoke/library/`) for registry-cached files

---

# evoke login
{: .no_toc }

Sign in to the hosted registry via Google OAuth (browser loopback + PKCE flow).

```console
$ evoke login [--registry <url>]
```

On success, stores access and refresh tokens in `~/.evoke/credentials.json`.

## Environment variables

| Variable | Default | Description |
|:---------|:--------|:------------|
| `EVOKE_REGISTRY_URL` | `http://localhost:8080` | Registry base URL |
| `GOOGLE_CLIENT_ID` | *(required)* | Desktop OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | *(required)* | Desktop OAuth client secret |

---

# evoke index
{: .no_toc }

Refresh the local SQLite file index by scanning all configured source roots.

```console
$ evoke index
```

The index stores tags and declarations for each `.evoke` file, enabling fast selector resolution in `evoke generate`.

## Output

```console
$ evoke index
indexing /Users/you/my-characters (configured)
indexing /Users/you/.evoke/library (library)
  /Users/you/my-characters (configured): 12 files
  /Users/you/.evoke/library (library): 3 files

total: 2 roots, 15 files
```
