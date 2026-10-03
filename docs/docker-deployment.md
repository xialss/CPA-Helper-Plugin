# Docker deployment

The local `cli-proxy-api` container is CPA v8.0.3, commit `acdace9`, Linux amd64,
Debian 12. The exact image ID is:
`sha256:69326f4bcf4f6e68a7a84885be8e89adb917ef47227a20f3cc9b405e7130bc50`.

## Installed layout

- Host plugin directory: `E:\CLIProxyAPI\plugins`
- Container mount: `/CLIProxyAPI/plugins`
- Library: `plugins/linux/amd64/cpa-helper-plugin-v0.1.0.so`
- State directory: `/CLIProxyAPI/plugins/cpa-helper-state`
- CPA console: `http://127.0.0.1:8317/management.html`
- Plugin UI: `http://127.0.0.1:8317/v0/resource/plugins/cpa-helper-plugin/ui`

The existing config, auth and log mounts were retained. The pre-upgrade config,
plugin and policy state were backed up under
`E:\CLIProxyAPI\temp\cpa-helper-before-v8-20260928`. Existing billing plugin
files and its disabled setting were preserved.

The Compose volume added to `E:\CLIProxyAPI\docker-compose.yml` is:

```yaml
- ${CLI_PROXY_PLUGIN_PATH:-./plugins}:/CLIProxyAPI/plugins
```

The replacement plugin was copied before recreation. Upgrade used the already
pulled image digest with `docker compose up -d --no-deps --pull never
cli-proxy-api`; the running service then reported CPA v8.0.3 and plugin 0.1.4.

## Official activation

After copying the library, authenticated `PUT
/v0/management/plugins/cpa-helper-plugin/config` was called with:

```json
{"enabled":true,"state_dir":"/CLIProxyAPI/plugins/cpa-helper-state"}
```

CPA hot-loads the plugin and publishes its menu in `GET /v0/management/plugins`.
Both `registered` and `effective_enabled` must be true. The embedded page requires
the existing CPA management key. Credentials are not included in this repository.

## Acceptance evidence

- The exact binary copied from the running container passed the compatibility
  fixture: dynamic loading, weighted subset routing, aliases, 403/429/503,
  streaming cancellation, restart and rollback.
- The actual service on port 8317 registered the plugin menu and reported healthy
  revisioned policy. Its real directory exposed one downstream key and 27 models.
- A temporary acceptance key with a deny rule for a registered model returned
  `403` with `model_forbidden`; no upstream generation was requested.
- The temporary key was removed and original empty rules restored by rollback.
  Revision 7 is the resulting unrestricted policy, not a deployed sample policy.
- Browser interaction was subsequently verified with Playwright/Edge against the
  real CPA management page: one CPA login with Remember Password, official plugin
  iframe navigation, masked downstream/upstream key display, inventory categories,
  and reload without plugin login. The test did not write any policy.

## Operational rollback

Disable only this plugin through authenticated `PATCH
/v8/management/config/plugins/configs/cpa-helper-plugin` with `{"enabled":false}`.
This removes its admission and routing restrictions; assess active policy first.
Retain the mounted state directory. Binary or state restoration requires stopping
CPA first; do not overwrite state underneath a running plugin.
