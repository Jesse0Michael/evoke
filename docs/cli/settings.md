# evoke settings

Manage user settings stored in `~/.evoke/settings.json`.

```console
$ evoke settings              # show current settings
$ evoke settings set <key> <value>
$ evoke settings remove <key> <value>
$ evoke settings remove <key>            # scalar keys clear without a value
```

## Keys

| Key | Description |
|:----|:------------|
| `path` | A source directory containing `.evoke` files. Multiple paths can be added. |
| `output_path` | The directory [`evoke view`](index.md) browses — where the backend saves what evoke generated. For a standard ComfyUI install this is `<ComfyUI>/output/images`. |

## Examples

Add a source path:

```console
$ evoke settings set path ~/my-characters
```

Remove a source path:

```console
$ evoke settings remove path ~/my-characters
```

Point the viewer at the backend's output directory:

```console
$ evoke settings set output_path ~/ComfyUI/output/images
```

Show all settings:

```console
$ evoke settings
{
  "paths": ["/Users/you/my-characters"],
  "output_path": "/Users/you/ComfyUI/output/images"
}
```

## Source roots

The `image` and `index` commands discover `.evoke` files from these roots (in order):

1. **`EVOKE_PATH`** environment variable (colon-separated directories)
2. **Configured paths** from `settings.json`
3. **Library directory** (`~/.evoke/library/`) for registry-cached files

## Output directory

`evoke view` resolves the directory it browses in this order, and guesses nothing
— where the backend writes is machine-specific, so an unconfigured viewer says so
rather than browsing a directory that happens to exist:

1. **`EVOKE_OUTPUT_DIR`** environment variable
2. **`output_path`** from `settings.json`

---

# evoke login

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

