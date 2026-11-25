# Releases

plsremind is released in three parts, each with its own version and changelog:

| Part | What it holds | Tag | Where it goes |
|---|---|---|---|
| The server | the hub with the dashboard, the Discord bot, the deploy bundle | `v0.3.1` | images on GHCR, the bundle on the GitHub release |
| The Python SDK | `sdk/python` | `sdk-python-v0.2.0` | PyPI, as `plsremind` |
| The TypeScript SDK | `sdk/ts` | `sdk-ts-v0.2.0` | npm, as `plsremind` |

A self-hoster only needs the server's version. The hub and the bot of one release belong
together and always carry the same number.

## What the numbers mean

Before 1.0, a release that only fixes things raises the last number (0.3.0 to 0.3.1), and a
release with new features or with breaking changes raises the middle one (0.3.1 to 0.4.0).

- For the server, a change is breaking when a self-hoster has to do something when
  updating: a changed or removed setting, a manual step. The release notes then say what.
- For an SDK, a change is breaking when code that used it has to change.

New fields and events in the API are not breaking; the SDKs and adapters ignore what they do
not know. Each SDK names the oldest server release it works with at the top of its README.

## Which releases get fixes

Only the newest release. A fix comes as a new patch release, never as a change to an existing
one: released images and packages are never rebuilt. Updates of the base images and of what
runs inside them come the same way, as patch releases.

Going back to an older release is not supported. Back up before you update; going back means
restoring that backup (see [self-hosting](self-hosting.md#updating)).

## How a release is made

Changes land on `main` through squash-merged pull requests whose titles are conventional
commits (`feat:`, `fix(hub):`, `docs:` ...); those titles become the changelog. release-please
keeps one release pull request per part open, with the changelog and the next version. Merging
it tags the release, and the release workflow publishes it: images for both architectures with
provenance, the bundle, or the SDK package. Before a server release, a test updates the previous
release, full of data, to the new one.

When publishing fails, rerun only the failed jobs of that run, within a week: after that, what
the run built has expired and the rerun fails too. Otherwise, or when a rerun fails, run the
release workflow by hand on `main` with the release's tag (`v0.2.1`, `sdk-python-v0.3.0` ...):
it publishes from the tagged commit, finishes what is half out and skips what is already out.

Rules for pull requests:

- A breaking change has `!` in its title (`feat(hub)!: ...`) and a `BREAKING CHANGE:` paragraph
  that says what to do. It touches only the part it breaks: an SDK break and a server break never
  share a pull request, or both would be announced as breaking.
- When an SDK starts to use something new in the hub, its README's "works with plsremind X or
  newer" goes up in the same pull request, and the server is released before that SDK.
