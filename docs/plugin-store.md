# Third-party plugin source

This repository publishes a CPA plugin registry at:

```text
https://raw.githubusercontent.com/xialss/CPA-Helper-Plugin/main/registry.json
```

The registry is intentionally third-party and is not submitted to the official
CLIProxyAPI plugin store. CPA still keeps its built-in official source; entries in
`plugins.store-sources` are appended to it.

## Configure CPA

Add the source to the CPA configuration:

```yaml
plugins:
  enabled: true
  dir: /CLIProxyAPI/plugins
  store-sources:
    - https://raw.githubusercontent.com/xialss/CPA-Helper-Plugin/main/registry.json
  configs:
    cpa-helper-plugin:
      enabled: true
      state_dir: /CLIProxyAPI/plugins/cpa-helper-state
```

In CPAMC, the same setting is available in the configuration editor under the
advanced plugin-source section. Save the configuration and refresh the plugin
store. A custom source is shown as third-party, so CPAMC may ask for an explicit
confirmation before installing it.

## Install on another machine

1. Use CPA v7.2.143 or v7.3.7 with dynamic plugins enabled.
2. Add the registry URL above to `plugins.store-sources`, or enter it in CPAMC.
3. Open the CPA/CPAMC plugin store and refresh it.
4. Install **CPA Helper**. CPA downloads the matching platform archive from the
   GitHub Release, verifies `checksums.txt`, installs the library, and writes the
   plugin configuration.
5. Keep `/CLIProxyAPI/plugins` mounted or otherwise persistent, then open the
   plugin resource at `/v0/resource/plugins/cpa-helper-plugin/ui`.

The embedded page reuses the existing CPA management session. It does not require
a second plugin login. The policy state directory must survive restarts and plugin
updates.

## Publish an update

Push a semantic version tag such as `v0.1.1` to this repository. The release
workflow builds Linux amd64 and Windows amd64 libraries and publishes these assets:

```text
cpa-helper-plugin_0.1.1_linux_amd64.zip
cpa-helper-plugin_0.1.1_windows_amd64.zip
checksums.txt
```

Each archive contains exactly one library at its root (`cpa-helper-plugin.so` or
`cpa-helper-plugin.dll`). CPA reads the latest GitHub Release for the repository,
so subsequent releases do not require a registry change. The plugin version is
injected from the release tag during the build.

On Windows, stop or restart CPA before replacing an already loaded plugin DLL. Keep
the existing state directory when upgrading; back it up before a binary or state
rollback.

## Private registry authentication

The repository is currently public so another CPA can fetch the registry and
release assets without credentials. If the repository is later made private, CPA's
`plugins.store-auth` rules can provide a GitHub token from an environment variable;
the token itself must not be placed in this repository or in `registry.json`.
