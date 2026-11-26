# Releasing

How a version tag becomes a signed GitHub Release (Gate G2) that includes the installer (Gate G3). The release path is pull-based: GitHub-hosted runners build and publish, nothing connects to a device (ADR 0003, ORB-CICD-005). What a user does with a release is in [`INSTALL.md`](INSTALL.md).

## What a release contains

| File | Purpose |
|---|---|
| `oarfish_<version>_linux_arm64.tar.gz` | Bundle: `oarfish_<version>_linux_arm64/oarfish` (static binary, frontend embedded), `oarfish.service`, since G6.3b also the update helper `oarfish-update-helper` and the units `oarfish-update-helper.socket`, `oarfish-update-helper.service` and `oarfish-update-recover.service`, since G7.3a also the privilege broker `oarfish-broker` and the units `oarfish-broker.socket` and `oarfish-broker.service` (see "Bundle shapes"), `LICENSE` and `THIRD_PARTY_NOTICES.txt` (licence texts of the Svelte runtime, the Vite helper snippet, the Go runtime and, since G5.1, `golang.org/x/crypto`, which are compiled into the binary and whose licences require the notice to travel with it) |
| `install.sh` | The installer ([`INSTALL.md`](INSTALL.md)): `install/install.sh` with the release version written into one line (below). Listed in `checksums.txt`, so the signature covers it |
| `manifest.json` | Machine-readable metadata (see below) |
| `checksums.txt` | SHA-256 of the bundle, `install.sh`, `manifest.json` and the SBOMs, in `sha256sum` format |
| `checksums.txt.sig` | minisign signature of `checksums.txt`, legacy mode: plain Ed25519 over the file bytes (ADR 0005) |
| `sbom-backend.cdx.json`, `sbom-frontend.cdx.json` | CycloneDX SBOMs (ORB-SEC-009, SOLL) |

`<version>` is the tag without the leading `v`. A tag with a hyphen (`v0.1.0-alpha.1`) is published as a prerelease.

The bundle contains `oarfish`, `oarfish.service` (the systemd unit the installer installs), `LICENSE` and `THIRD_PARTY_NOTICES.txt`. The manifest schema did not change for the installer: `install.sh` is a release asset next to the bundle, covered by `checksums.txt`, not part of the tarball, so the manifest's `contents` do not list it. `bundlegen check` requires it in `checksums.txt` and checks its stamp instead.

`manifest.json` fields: `schema_version`, `name`, `version`, `tag`, `prerelease`, `commit` (40 hex), `build_date` (RFC 3339, equal to the commit timestamp so the file is reproducible), `os`, `architecture`, `requirements` (`os`, `architecture`, `cpu`, `libc`, `runtime_dependencies`), `bundle` (`file`, `sha256`, `size`) and `contents` (path, mode, size, SHA-256 of every file in the tarball). Kernel and memory minimums are not included yet because they have not been measured on a device (ORB-REL-002 is met for architecture, OS and libc only). Three optional fields are added by ADR 0010 and described in the next section; a manifest without them is exactly the manifest above.

## Bundle shapes (ADR 0010, steps G6.2 and G6.3b, amendment A20)

The release tooling knows two bundle shapes. `bundlegen check` accepts exactly these two and nothing in between.

| Shape | Tarball entries (root directory `oarfish_<version>_linux_arm64/`) | Manifest |
|---|---|---|
| **Legacy** (explicit opt-out only) | the root directory and the four files above: `LICENSE` 0644, `THIRD_PARTY_NOTICES.txt` 0644, `oarfish` 0755, `oarfish.service` 0644 | no `components` object |
| **Components** (the default since G6.3b; twelve entries since G7.3a) | the five entries of the legacy shape plus exactly seven more, the component set: `oarfish-update-helper` 0755 (an ARM64 ELF executable) and the units `oarfish-update-helper.service`, `oarfish-update-helper.socket` and `oarfish-update-recover.service`, each 0644; and `oarfish-broker` 0755 (an ARM64 ELF executable) and the units `oarfish-broker.service` and `oarfish-broker.socket`, each 0644 | the object `components` is present, and `contents` lists the seven files like every other file |

The components shape of before G7.3a (nine entries, without the broker's three files) is no longer produced and is refused by `bundlegen check`, by `relverify` (the helper and the service) and by the installer, like a bundle with only some of the seven component files (ADR 0010 A20.4). This is class C evidence (tests and CI); no bundle of the new shape has been installed on a device yet.

**What the installer does with the twelve-entry bundle (G7.3b).** It stores the seven component files below `/opt/oarfish/components/<id>`, links the helper as before and the broker into `components/active` (`/usr/libexec/oarfish-broker` and the two broker units in `/etc/systemd/system`), creates the relative wants link of `oarfish-broker.socket` (none for the broker service), starts the broker socket, restarts it only when its text changed, and requires it to be active in the health check; `--uninstall` removes the broker's links again ([`INSTALL.md`](INSTALL.md#the-update-helper-layout-g63b)). This installer run is the only way a device gets the broker: the first release that carries it needs one installer run on every device (ADR 0011 B1, consequence 1; D-C87 item 18), also on devices of `v0.1.0-alpha.12`. That release is step G7.6b; its notes are in [Release notes for the first broker release](#release-notes-for-the-first-broker-release), which also says what a device of `v0.1.0-alpha.12` shows for it and how such a device learns that it needs the installer. `sudo sh install.sh --dry-run` prints these steps for the components shape without downloading anything (the shape is known only after the download, so the plan lists the components steps separately). Class C only (fake root, stub `systemctl`, the arm64 containers); not verified on a device.

**Since G6.3b the default output of `scripts/release-bundle.sh` is the components shape, and so is what `ci.yml` and `release.yml` build** (neither workflow was changed: they call the script without arguments). `install/install.sh` installs both shapes and accepts exactly these two ([`INSTALL.md`](INSTALL.md#the-update-helper-layout-g63b)). The helper in the bundle is the update helper of `backend/cmd/oarfish-update-helper` (the stub of G6.3a was replaced from G6.5 on; the component transaction of G6.10 is in the code since step b and in the releases since `v0.1.0-alpha.11`). The legacy shape stays reachable only through the explicit opt-out `OARFISH_BUNDLE_SHAPE=legacy` (for tests and comparison); with it, `VERSION=0.0.0-g63a` and `COMMIT=ef7441cb5615a0ae35f846c4a38866a9f8dc6ec4` produce the same tarball and manifest as G6.3a (sha256 of the tarball `f7d76eeceb5afe600e958d71904691e1deb415c2207500c093e96dbe44b8dce4`, of the manifest `b83a74bf2db3932a50d2b7e81b64ed5c1280719faf360ec083300190773b48f9`; `install.sh` differs, as the installer changed). `scripts/bundlegen/testdata/legacy/` still holds the reference output of the tool before G6.2 and a test compares against it. The components bundle is exercised by the installer tests with fake files and by the CI jobs below; none of it is evidence that a helper works on a device, except that a test release in the components shape (throwaway key, test copy of the installer, never published) was installed on one device at the G6.3 owner stop (device observation, 2026-11-03; see [`INSTALL.md`](INSTALL.md#results-on-the-reference-device-g63)). The production key path with the real installer was not exercised there, and nothing is Verified.

Additive manifest fields (all optional, all absent from a legacy manifest, `schema_version` stays 1; the readers on the device (the update helper and the service, later steps) ignore fields they do not know, while `bundlegen check` rejects them):

| Field | Type and rule | Meaning |
|---|---|---|
| `requirements.min_installed_version` | SemVer (no leading `v`), not greater than `version` | The oldest installed version the update helper accepts an update from |
| `requirements.helper_protocol` | integer, at least 1; not greater than `components.protocol` when both are present | The lowest helper protocol the release's service needs |
| `components` | object, present if and only if the seven component files are in the bundle | Describes the helper the bundle delivers (its fields are about the helper; the broker adds none) |
| `components.key_id` | 16 lowercase hexadecimal digits | The key id embedded in the delivered helper. (Key id files and documents often print it in upper case; the manifest and `-expect-key-id` use lower case.) |
| `components.protocol` | integer, at least 1 | The highest protocol the delivered helper implements |

Building the components shape. `bundlegen bundle` takes the seven files, the protocol and the key id as one set: all nine flags or none. A partial set is an error, so a missing helper or broker never silently yields the legacy shape. Without the nine flags the output is the legacy bundle.

| Flag | Meaning |
|---|---|
| `-helper`, `-helper-service`, `-helper-socket`, `-recover-service` | Paths of the helper's four component files (the helper must be an ARM64 ELF) |
| `-broker`, `-broker-service`, `-broker-socket` | Paths of the broker's three component files (the broker must be an ARM64 ELF), since G7.3a |
| `-components-protocol N` | `components.protocol` |
| `-components-key-id HEX16` | `components.key_id` |
| `-min-installed-version V`, `-requires-helper-protocol N` | Optional, independent of the set above: `requirements.min_installed_version` and `requirements.helper_protocol` (both are valid in either shape) |
| `check -expect-key-id HEX16` | `bundlegen check` requires `components.key_id` to equal this value; a release without `components` then fails. `scripts/release-bundle.sh` passes it in the default shape (below) |

`scripts/release-bundle.sh` selects the shape:

| Setting | Result |
|---|---|
| nothing set (the default) | **Components shape.** The script builds the helper with `scripts/build-helper.sh` and the broker with `scripts/build-broker.sh` (both without the release `VERSION` and `COMMIT`, see "The helper does not carry the release version" below), takes the three helper units and the recover unit and the two broker units from `install/`, sets `components.protocol` to 1 and derives `components.key_id` from `release/oarfish-release.pub` with the method of `scripts/check-helper-key.sh` (the 16 digits of the first line, in lower case; `11f1c52777090bfc` for the release key). After the build it runs `bundlegen check -expect-key-id` with that value on a throwaway copy of the three files with a generated `checksums.txt`; a different key id fails the build. This is where the key id is checked: the installer does not compare it with its own (the installer tests use a throwaway installer key while the helper embeds the release key). |
| `OARFISH_BUNDLE_SHAPE=legacy` | The legacy bundle, byte for byte as before ADR 0010. Cannot be combined with `OARFISH_COMPONENTS_*`. |
| `OARFISH_COMPONENTS_DIR`, `OARFISH_COMPONENTS_PROTOCOL`, `OARFISH_COMPONENTS_KEY_ID` (any of them) | Their G6.2 meaning, all or nothing (a partial set is refused by `bundlegen`); the directory holds the seven component files since G7.3a; nothing is built or derived by the script and no key id check runs. |
| `OARFISH_MIN_INSTALLED_VERSION`, `OARFISH_REQUIRES_HELPER_PROTOCOL` | Independent of the above; both are valid in either shape. |
| any other value of `OARFISH_BUNDLE_SHAPE` | An error. |

The release workflow's dry run (and `release.yml`) therefore builds, signs with the throwaway or production key as before, and verifies the components shape; the size budget (35,000,000 bytes) applies to the bundle with the helper in it. Reproducibility is checked on the components bundle by `scripts/check-reproducible.sh`.

**The helper does not carry the release version** (D-C80, ADR 0010 A13). The update helper on the device compares the helper, its three units and `oarfish.service` of a staged release with the installed files by SHA-256 and answers `installer_required` when one differs. (Since G7.3a it compares eight files: the broker and its two units as well, ADR 0010 A20; the broker's bytes depend only on its code and the toolchain in the same way, `scripts/check-broker-independent.sh`.) So the helper's bytes depend only on its code, the packages it links and the Go toolchain: `scripts/build-helper.sh` passes no `-X` flag, the helper does not link `backend/internal/version`, and `helper_version` in its `status` answer is the constant `protocol-1`, not the release version (the main binary `oarfish` keeps its version). A release that does not change the helper code, a package it links, the Go version or a unit text therefore needs no installer run. A release that changes one of them needs one installer run on a device whose helper is older than G6.10 step b; a device with the helper of step b replaces them from the GUI after a second confirmation (ADR 0010 A16, E1), and the release notes say what changes (see [Release notes for G6.10](#release-notes-for-g610)). A device on `v0.1.0-alpha.7` needs one installer run for the next release, because that helper embeds its release version. Check it before tagging:

```bash
scripts/check-helper-independent.sh   # two helper builds with different VERSION and COMMIT are identical
make check-reproducible               # also: two bundles of different versions carry the same eight compared files
```

CI runs both in the job "Reproducible bundle". To see whether a release changes the components compared with the one before, compare the `contents` entries of the eight compared files (the seven files of the component set and `oarfish.service`, ADR 0010 A20) in the two manifests. The first components bundle measured for this change was 4,361,895 bytes against 3,008,364 bytes for the legacy bundle of the same commit (the helper stub is 3,342,496 bytes uncompressed).

`bundlegen check` rejects, in addition to everything it rejected before: a missing or extra tarball entry in either shape, a wrong mode, a helper that is not an ARM64 ELF (the installer's header check: 64-bit, little-endian, machine `0xB7`), `components` without the files or the files without `components`, a malformed `key_id`, a protocol below 1, `requirements.helper_protocol` greater than `components.protocol`, an invalid or too new `min_installed_version`, a path listed or stored twice, and any difference between `contents` and the tarball. It also rejects unknown manifest fields, as before G6.2: it validates the manifest the release tooling wrote, so every field must be one this tool knows. This is not the rule for the readers on the device, which ignore unknown fields so that additive changes stay compatible (ADR 0010, U4). The main binary is still not checked for its ELF header by `bundlegen`; the installer does that.

## One-time setup: signing key (maintainer, local machine)

The private key must never enter the repository, a log or a chat. The implementation agent does not generate or read it. Run these on your own machine:

```bash
brew install minisign        # macOS; Debian/Ubuntu: sudo apt install minisign

# 1. Key pair. The public key is written into the repository working tree,
#    the private key outside it. -W = no password (see ADR 0005).
mkdir -p -m 700 ~/.config/oarfish-release
minisign -G -W -p release/oarfish-release.pub -s ~/.config/oarfish-release/oarfish-release.key

# 2. Store the private key as a repository secret, read from the file (not printed).
gh secret set OARFISH_RELEASE_SIGNING_KEY --repo swingstate/oarfish < ~/.config/oarfish-release/oarfish-release.key
gh secret list --repo swingstate/oarfish

# 3. Back up ~/.config/oarfish-release/oarfish-release.key in a password manager.
#    Losing it means devices that embed the public key cannot accept new releases.
```

Commit only `release/oarfish-release.pub`. `*.key` is git-ignored as a safety net. Recommended, not configured by this gate: a tag protection rule for `v*` so only maintainers can create release tags.

## Dry run

Actions → Release → Run workflow, or:

```bash
gh workflow run release.yml --ref <branch>
```

It runs CI, builds the bundle, runs the two release checks ([Release checks](#release-checks-d-c92-and-the-two-step-rule)), checks reproducibility, size budgets and the ARM64 smoke test, generates SBOMs and checksums, signs with a throwaway key and verifies. A manual run is always a dry run, and so is the run triggered by a pull request that touches the release files. It uses version `0.0.0-dryrun.<sha>` and publishes nothing; the signed output is discarded. The job that references the release secret (`sign`) is skipped in these runs, so the secret is never sent to a runner; they use the job `sign-dry-run`.

## Release checks (D-C92 and the two-step rule)

Two checks compare the release being built with the releases before it. They run in the job `build` of `release.yml`, right after the bundle is built and before anything is signed, on a tag push and in every dry run. Both are class C: they are tested against fixtures only (`scripts/test-release-history.sh`, `scripts/bundlegen/history_test.go`) and ran once by hand against the published releases (below). Their first run on a real tag is the release of G7.6b.

**Which releases count, and which is "directly preceding".** The release tags of the checkout (every tag that matches the tag rule of the workflow), ordered by creation: the tagger date of an annotated tag, the commit date of a lightweight one (the alpha tags are of both kinds), ties by name. The tags created before the tag being built are the earlier releases; in a dry run, whose tag is not a tag of the repository, all of them. The last one is the directly preceding release. A shallow clone or a clone without tags fails both checks, because it would look like a repository without releases; the job `build` checks out with `fetch-depth: 0`. Locally: `git fetch --tags`.

**1. No older component set (D-C92, path A).** `scripts/check-component-history.sh` with `bundlegen history`.

- What it compares: the seven files of the component set (ADR 0010 A20.1). The manifests carry no set hash, so the check computes the set hash of A20.3 (SHA-256 over the seven SHA-256 digests in the order of A20.1) from the per-file digests in `contents`, for the release being built and for every earlier release; two sets are equal if and only if all seven digests are equal. The printed hash is the one the helper records as `components_set_sha256`.
- The earlier manifests are downloaded anonymously, `https://github.com/swingstate/oarfish/releases/download/<tag>/manifest.json`, without a token (also in the workflow; `OARFISH_RELEASES_URL` overrides the base for tests). At most 1 MiB, https only, also for redirects.
- The rule: equal to the set of the directly preceding release passes (unchanged); equal to the set of any other earlier release fails; otherwise it passes. A legacy manifest (no `components`) and a four-file set (before G7.3a) are not comparable and are skipped with a line each; a skipped preceding release leaves no set that may be equal.
- Fail closed: a manifest that cannot be downloaded, a missing tag on the release page, or a manifest that is malformed (not JSON, another tag, a bad digest, a path twice, component files without `components`, a set of neither four nor seven files) fails the job. Nothing is skipped because it could not be read.
- The lines it prints, and a failure:

  ```text
  component-set: v0.1.0-alpha.12: skipped, four-file component set (before G7.3a), not comparable
  component-set: directly preceding release: v0.1.0-alpha.12
  component-set: PASS: no earlier release carries a seven-file set; nothing to compare

  bundlegen: component-set: FAIL: v0.1.0-alpha.15 brings the component set of v0.1.0-alpha.13, which is not the directly preceding release v0.1.0-alpha.14. D-C92: no release brings an older component set. An exception needs the owner's approval in chat for this tag, a line in release/release-exceptions.txt and a sentence in the release notes (docs/RELEASING.md)
  bundlegen: component-set: FAIL: cannot read the manifest of v0.1.0-alpha.3: HTTP status 404 (the check fails closed, D-C92)
  ```

**2. The two-step rule (ADR 0010 U15 rule 3, A15, D-C83).** `scripts/check-unit-forms.sh`.

- What it compares: the unit files of the bundle with the unit lint of the update helper of the directly preceding release, which is what that helper runs when a device stages this release. The lint is taken from the repository history, not downloaded: the script extracts `backend/` at the preceding tag, leaves out the helper's own tests, adds one test that calls that source's `lintUnit` for every name in its `lintedUnits`, and runs it with this repository's Go toolchain, offline (`GOPROXY=off`, an empty module cache). No helper binary is downloaded or run.
- A change in comments, blank lines, `Description=` or `Documentation=` passes, because the lint removes them; a text equal to an accepted form of that helper (for `oarfish.service`: within its rule table) passes; every other text fails. A unit that helper does not lint (the two broker units, for a helper before G7.3a) is listed and not judged. A preceding helper without the lint (before G6.10 step a, up to `v0.1.0-alpha.10`) answers `installer_required` for every difference; the check says so and passes.
- Fail closed: a preceding source that cannot be extracted or built, a unit that the preceding helper lints and the release lacks, or a result that names no unit fails the job.
- The lines it prints, and a failure:

  ```text
  unit-forms: oarfish-update-helper.service: accepted by the unit lint of the helper of v0.1.0-alpha.12
  unit-forms: oarfish-broker.socket: not linted by the helper of v0.1.0-alpha.12 (not a unit of its component set), not judged
  unit-forms: PASS: every unit that the helper of v0.1.0-alpha.12 lints passes its lint

  error: unit-forms: FAIL: oarfish-update-helper.service (form_mismatch) of v0.1.0-alpha.15 would be refused by the helper of v0.1.0-alpha.14 (unit_rejected on a device that stages it). Two-step rule (ADR 0010 U15 rule 3, A15, D-C83): first release a helper that lists the new form in unitForms (or the row in serviceRules), then change the unit (docs/RELEASING.md)
  ```

**The exception.** Both checks accept one exception per tag and check, and only from the committed file [`release/release-exceptions.txt`](../release/release-exceptions.txt), which the workflow passes with `--exceptions`. A line is `TAG CHECK REASON`, with `CHECK` either `component-set` or `unit-forms`; the file holds no active line by default. The owner's explicit action for that tag is needed twice: the approval in chat for that tag (D-C92 item 2), and the merge of the pull request that adds the line, before the tag is pushed. Tag names are never reused, so a line cannot apply to another release; a malformed line, an unknown check, a missing reason or a second line for the same tag and check fails the check. With an exception the check passes and prints `EXCEPTION:` with the files and the reason. The release notes of that release say so (a sentence that names the older release whose set returns, or the unit a device on the previous release refuses and that such a device needs one installer run). Nothing else switches an exception on: no workflow input, no environment variable, no default.

**A tag whose release was never published (open point for the owner).** Both checks read the manifest of every earlier release tag and fail when one cannot be read; they do not retry (D-C99, fail closed). If the release job failed after the tag was pushed and no release exists for that tag, its manifest cannot be read, and both checks fail for every later release. The usual repair is to run the failed jobs of that tag again (as for the 504 of `v0.1.0-alpha.8`), which publishes the missing release. A tag is never deleted or reused (AGENT_RULES 10.9). If a tag can never be published, the checks need a change, and the owner decides which; an exception kind for an unreadable tag is not built. **Open:** whether such a change is wanted, and what it would be.

**Run them locally** (before tagging, after `git fetch --tags`):

```bash
make release-bundle            # the bundle of this commit in dist/release
make check-release-history     # both checks against dist/release (downloads the earlier manifests)
make test-release-history      # the fixture tests (also part of make test-release-tools)
```

CI runs the fixture tests in the job "Signing and tamper tests" and the Go tests of `bundlegen history` in "Go checks"; the release workflow runs both checks against the real releases.

**Verification status.** Class C only. Read-only run against the published releases on 2026-11-26, for the bundle of `main` at the R-check base (`c2e86f9c3e7e6ef8dc769843f750d91a7d42d29f`): `v0.1.0-alpha.1` to `v0.1.0-alpha.6` skipped (legacy), `v0.1.0-alpha.7` to `v0.1.0-alpha.12` skipped (four-file sets), nothing comparable, PASS; the four units that the helper of `v0.1.0-alpha.12` lints passed its lint and the two broker units were listed as not linted, PASS. Not verified: a run on a real tag (the first is G7.6b), a real network failure in the workflow, a comparable earlier set (no published release has a seven-file set yet).

## Release procedure

1. Merge the change to `main`; wait for CI.
2. **Before tagging the first release that reports the process ID in `/healthz` and serves `/readyz`** (ADR 0010, A11.2, D-C68): confirm or adjust the constant `strictHealthFrom` in `backend/cmd/oarfish-update-helper/health.go` so that it equals the version of that release (it is set to `0.1.0-alpha.7` at the time of writing). A wrong value either skips the stricter health check for that release or makes the helper refuse a healthy one.
3. **Every release after the bootstrap (`v0.1.0-alpha.8`)**: compare the files the update helper compares (`oarfish-update-helper`, `oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, `oarfish.service`, and since G7.3a also `oarfish-broker`, `oarfish-broker.socket` and `oarfish-broker.service`: eight files, ADR 0010 A20) with the previous release before tagging. Build the bundle of the commit (`make release-bundle`) and compare the `sha256` of these entries in `contents` of its `manifest.json` with the published `manifest.json` of the previous release. All equal: a device on the previous release updates from the GUI without a second confirmation. One differs: a device whose helper is older than G6.10 step b (every release up to and including `v0.1.0-alpha.10`) answers `installer_required`, and the release notes must say that this release needs one installer run on such a device (`sudo sh install.sh` from the release page), and why. A device with the helper of step b applies it from the GUI with the second confirmation; the release notes name what changes ([Release notes for G6.10](#release-notes-for-g610)). Devices on `v0.1.0-alpha.7` or earlier always need one installer run ([`UPDATE.md`](UPDATE.md#when-the-installer-is-required)); say that in the notes as well. `components.protocol` of the release must be the protocol of the helper of the release before it, or one more (D5); otherwise a device answers `requirements_not_met` and needs the installer. The release check of D-C92 ([Release checks](#release-checks-d-c92-and-the-two-step-rule)) prints the set hash of the seven component files for this release and for the previous one, so "the seven are unchanged" can be read from its log line `brings the set of the preceding release ... unchanged`; which file differs, and `oarfish.service`, are still compared by hand as above. A release whose seven files equal those of an older release that is not the previous one fails that check (D-C92).
4. **A release that changes one of the five component units** (`oarfish-update-helper.socket`, `oarfish-update-helper.service`, `oarfish-update-recover.service`, and since G7.3a `oarfish-broker.socket`, `oarfish-broker.service`) in more than comments, blank lines, `Description=` or `Documentation=`, or that gives `oarfish.service` a directive or a value outside the lint table: the two-step rule (ADR 0010 U15 rule 3, amendment A15, D-C83). The update helper on a device lints the units of the release it stages with the tables compiled into the installed helper, and answers `unit_rejected` for a text it does not know. A component unit must equal one of the accepted forms in `backend/cmd/oarfish-update-helper/unitrules.go` (`unitForms`, named `helperForms` before G7.3a: at most two per unit, the current one and the next one). Checklist:
   - [ ] **Step one, release N.** Add the new form as the second entry of the unit's list in `unitForms` (for `oarfish.service`: add the row to `serviceRules`), with an ADR amendment or a decision. The unit files in `install/` stay as they are, so release N still passes the lint of the helper before it. Release N changes the helper's bytes, so it is a release of item 3: a device with a helper older than G6.10 step b needs one installer run for it, a device with the helper of step b takes it from the GUI with the second confirmation.
   - [ ] **Step two, release N+1 or later.** Change the unit file in `install/`. `TestInstallUnitsAreTheFirstForms` requires the shipped unit to be the first entry of its list, so the same change makes the new form the first entry and keeps the old one as the second; the old form is removed only by a later amendment or decision.
   - [ ] `make check-helper` and the helper tests are green: `TestInstallUnitsPassTheLint` proves that the units and the tables of this commit agree. The units of this release against the helper of the previous release are checked by `scripts/check-unit-forms.sh` in the release workflow, before signing ([Release checks](#release-checks-d-c92-and-the-two-step-rule)): it runs the lint of the previous release's source over the unit files of the bundle and fails on a text that helper does not accept. Run it before tagging with `make release-bundle && make check-release-history`. Step one passes it (the units are unchanged); step two passes it only when release N is the previous release.
   - [ ] A device whose helper does not list the new form cannot take the release from the GUI; the release notes say that it needs one installer run (the installer does not lint). A release that changes the socket unit text says in its notes that the new text takes effect at the next boot or installer run (ADR 0010 A2, A9).
   - Until the first release whose helper has the lint is installed on a device, this item changes nothing there: an earlier helper answers `installer_required` for every difference (item 3).
   - The release notes of step two say which unit changes and, for the socket unit, that its new text takes effect after the next reboot of the device (below).
5. Tag a commit that is on `main` and push the tag:
   ```bash
   git tag v0.1.0-alpha.1 && git push origin v0.1.0-alpha.1
   ```
   The workflow rejects tags that are not `vMAJOR.MINOR.PATCH[-prerelease]` and tags whose commit is not on `main`.
6. Watch it: `gh run watch`. Jobs: `ci` (the full CI workflow) → `build` → `sign` → `publish`. Only `sign` references the secret and it only starts for a tag push; only `publish` has `contents: write`.
7. Verify the published assets (below).
8. **Release notes of the G5.1 release (and any release a device may reach from `v0.1.0-alpha.5` or earlier)** must say: "After this update the device has no administrator account and starts in setup mode. The GUI shows the setup page; read the setup token on the device with `sudo cat /var/lib/oarfish/setup-token`. The dashboard and the device page show data only after setup and login. Every `/api/v1` route except the session status, login and setup now needs a session; `/healthz` stays public." (ADR 0009, Consequences; D-C46)

Publishing happens only from a tag push; there is no manual publish path. If a release fails halfway, delete the draft/partial release and the tag, fix the cause and tag again. Never reuse a published version.

## Checklist for the release step G7.6b

The first broker release is made by its own step, G7.6b, with the procedure above. This is what that step does, in order; it adds no step of its own and points to the procedure for each one.

1. **Precondition.** The owner reported the merge of the documentation step G7.6a in chat and confirmed the exact tag name, a name that was never used (AGENT_RULES 10.7). Without both, nothing below starts.
2. Procedure item 1: the change is merged to `main`, and CI on `main` is green.
3. Procedure items 3 and 4: compare the eight compared files with the previous release and apply the two-step rule. For this release the three broker files are new against `v0.1.0-alpha.12`, so a device of that release needs the installer (procedure item 3); the notes of this release are the section [Release notes for the first broker release](#release-notes-for-the-first-broker-release).
4. Locally, after `git fetch --tags`: `make release-bundle && make check-release-history` ([Release checks](#release-checks-d-c92-and-the-two-step-rule)). Read both outputs.
5. **No exception line.** `release/release-exceptions.txt` holds no active line, unless the owner approved an exception for this tag in chat and merged the pull request with that line before the tag is pushed (D-C96). A line is not added by this step.
6. Procedure item 5: the tag is created on the full commit ID of the commit on `origin/main` and pushed, with the confirmed tag name.
7. Procedure item 6: `gh run watch`. **Both release checks pass in the real run**; their log lines are the first ever from a real tag. If a job fails, the step stops and reports (AGENT_RULES 10.10); it does not retry with changes and changes no workflow file.
8. **The broker files are in the bundle and the set is new.** The published `manifest.json` lists the twelve entries, including `oarfish-broker`, `oarfish-broker.service` and `oarfish-broker.socket`, with `components.key_id` equal to the key id of `release/oarfish-release.pub`; the log line of the component-set check prints a set hash that differs from the set hash of every earlier release.
9. Append the notes with `gh release edit`: the section [Release notes for the first broker release](#release-notes-for-the-first-broker-release) with `<TAG>` replaced, and the further texts that procedure item 3 or 4 requires.
10. Procedure item 7: verify the published assets, anonymously ([Verify a release anonymously](#verify-a-release-anonymously)); `scripts/verify-release.sh` prints `VERIFIED`.
11. Report, with the evidence class: the release is class C and anonymous verification only. Nothing of the broker is Verified, and the device evidence is the step G7.6c.

## Release notes for the first broker release

These texts go into the release notes with `gh release edit` after the release workflow (AGENT_RULES 10.8, item 9 of the checklist above). Replace `<TAG>` with the tag of the release, which is known only in step G7.6b. Keep the meaning.

**What a device of `v0.1.0-alpha.12` shows for this release.** Read from the code of the tag `v0.1.0-alpha.12`; not run on a device with this release, which does not exist yet.

- The service verifies the three small files of the newest release when the update page checks (`doCheck`, `advisory`, `verifyMetadata` in `backend/internal/updater/check.go`) with `relverify.VerifyMetadata`. Its layout allows nine entries for the components shape (`layout` in `backend/internal/relverify/layout.go`); the manifest of this release lists twelve, so `buildContents` in `manifest.go` answers `manifest_invalid`.
- The card "Verfügbare Version" (`Update.svelte`) shows the version and "Vorabversion", the download size `0 B` (it is not filled), the signature as "Nicht bestätigt", and a critical notice with two sentences: "Die Beschreibung des Releases (Manifest) ist ungültig. Diese Version wird nicht installiert." (`updErr.manifest_invalid`) and "Diese Version wird nicht angeboten." (`updSignatureFailedNote`). It shows the release notes below, labelled "Von GitHub, nicht signiert". The English page reads "The release description (manifest) is not valid. This version is not installed." and "This version is not offered."
- There is no button to apply it: `canApply` needs `verified`. `stage` never runs, so the installer view ("Installer nötig", `InstallerView`) is never built; it exists only for a refusal of `stage` (`installerCase`). **The page shows no hint about the installer.** The release notes on that card are the only text of this release that such a device shows, so the first lines of the notes carry the hint. This is the only channel the code of that release offers for the question that this document and `UPDATE.md` left to step G7.6 (how a device of `v0.1.0-alpha.12` learns that the first broker release needs the installer); whether another way is wanted is for the owner.
- The code of releases before `v0.1.0-alpha.12` was not read for this. Whatever those devices show, every device needs the installer.

**English**

> This release (`<TAG>`) brings the privilege broker and the new page "Aktivität". Every device needs one run of the installer to get it: the update helper of earlier releases cannot install the broker. Download `install.sh` from this release page, read it, and run it on the device with `sudo sh install.sh`. Your settings and your account are kept.
>
> A device with `v0.1.0-alpha.12` cannot take this release from its update page. The page lists it as "Nicht bestätigt" and says that the release description (manifest) is not valid and that this version is not offered. That is expected: the old update page does not know the new layout of the release, and it gives no hint about the installer. The message does not mean that your device is damaged. Use the installer as described above.
>
> New: the page "Aktivität" shows the services of the device, and you can start, stop and restart them, and restart or shut down the device, each after you enter your password. The broker that does this is a separate process with root rights and a short fixed list of actions; the Oarfish service itself keeps running without root rights. The release process also has two new checks: no release may bring an older set of the update helper and the broker than an earlier release, and a changed unit file must be accepted by the update helper of the release before it.
>
> Known limit: if the first run of the installer is interrupted (for example by a power loss), run the installer again. In the rare case that the update page then reports a damaged known-good set of the update helper, remove `/var/lib/oarfish-update/state.json` with `sudo rm` while no update is running, and run the installer again.
>
> Not verified: nothing of the broker has been verified on a device at the time of this release. Evidence from a device follows in the next step (G7.6c). The reference device stays "Compatible".

**German**

> Dieses Release (`<TAG>`) bringt den Privilege Broker und die neue Seite „Aktivität". Jedes Gerät braucht einmal den Installer, um ihn zu erhalten: Der Update Helper früherer Releases kann den Broker nicht installieren. Laden Sie `install.sh` von dieser Release-Seite, lesen Sie es und führen Sie es auf dem Gerät mit `sudo sh install.sh` aus. Ihre Einstellungen und Ihr Konto bleiben erhalten.
>
> Ein Gerät mit `v0.1.0-alpha.12` kann dieses Release nicht über seine Update-Seite installieren. Die Seite zeigt es als „Nicht bestätigt“ und meldet, dass die Beschreibung des Releases (Manifest) ungültig ist und diese Version nicht angeboten wird. Das ist zu erwarten: Die alte Update-Seite kennt den neuen Aufbau des Releases nicht und gibt keinen Hinweis auf den Installer. Die Meldung bedeutet nicht, dass Ihr Gerät beschädigt ist. Nutzen Sie den Installer wie oben beschrieben.
>
> Neu: Die Seite „Aktivität“ zeigt die Dienste des Geräts. Sie können sie starten, stoppen und neu starten sowie das Gerät neu starten oder herunterfahren, jeweils nach Eingabe Ihres Passworts. Der Broker, der das ausführt, ist ein eigener Prozess mit Root-Rechten und einer kurzen, festen Liste von Aktionen; der Oarfish-Dienst selbst läuft weiterhin ohne Root-Rechte. Der Release-Prozess hat außerdem zwei neue Prüfungen: Kein Release darf einen älteren Satz aus Update Helper und Broker bringen als ein früheres, und eine geänderte Unit-Datei muss vom Update Helper des vorherigen Releases akzeptiert werden.
>
> Bekannte Grenze: Wird der erste Lauf des Installers unterbrochen (zum Beispiel durch einen Stromausfall), führen Sie den Installer erneut aus. Meldet die Update-Seite danach im seltenen Fall einen beschädigten bekannten guten Stand des Update Helpers, entfernen Sie `/var/lib/oarfish-update/state.json` mit `sudo rm`, solange kein Update läuft, und führen Sie den Installer erneut aus.
>
> Nicht verifiziert: Zum Zeitpunkt dieses Releases ist nichts vom Broker auf einem Gerät verifiziert. Nachweise vom Gerät folgen im nächsten Schritt (G7.6c). Das Referenzgerät bleibt „Compatible“.

## Release notes for G6.10

These texts go into the release notes with `gh release edit` after the release workflow (AGENT_RULES 10.8). Adapt the versions; keep the meaning. The first release that carries it is `v0.1.0-alpha.11`; its notes listed what was not verified at that time. Since then the GUI update from `v0.1.0-alpha.11` to `v0.1.0-alpha.12` was observed on one device (class D, not Verified; [`UPDATE.md`](UPDATE.md#v010-alpha12-class-d)).

**The first release that carries the helper of G6.10 step b** (every device needs one installer run; used for `v0.1.0-alpha.11`, whose published notes also named the file that differs from `v0.1.0-alpha.10` and what was not verified at that time):

> This release replaces the update helper with a version that can update itself and its units from the GUI. Every device needs one run of the installer to get it: the update helper of every earlier release cannot replace itself, and the update page shows "Installer nötig" for this release. Download `install.sh` from this release page, read it, and run it on the device with `sudo sh install.sh`. Your settings and your account are kept. From the next release on, a release that changes the update helper or its units is installed from the GUI after a second confirmation.

Before this release was tagged: the observation on the device that the helper can read its own executable image under its sandbox (ADR 0010 D1), and item 3 above. For `v0.1.0-alpha.11` D1 was inferred from `c_verified` in one component update with a test build (class T, kit Phase 5), and later with the production key in the GUI update to `v0.1.0-alpha.12` (class D; [`UPDATE.md`](UPDATE.md#results-on-the-reference-device)).

**A later release that changes components** (the update helper, one of its units, or `oarfish.service`; item 3 shows which of the eight compared files differ):

> This release also replaces privileged components of Oarfish: <the names, for example the update helper (`oarfish-update-helper`) and its socket unit (`oarfish-update-helper.socket`)>. On a device with the update helper of `v0.1.0-alpha.11` or later, the update page asks for a second confirmation that names them, and for the password again. If the update fails, the previous version is restored; a new update helper that passed its own check stays installed. A device with an older update helper needs one run of the installer (`sudo sh install.sh` from this release page).

Add, when the socket unit `oarfish-update-helper.socket` changes in more than comments, `Description=` or `Documentation=` (ADR 0010 A2, A15 D4):

> The new socket unit of the update helper takes effect after the next reboot of the device. Until then the update page shows a notice. Nothing else is needed.

Add, when a helper unit changes in a directive: the release follows the two-step rule of item 4, and the notes of step two say that a device whose helper does not list the new form needs one installer run (the update page shows "Installer nötig" with the reason that a unit file does not match the rules of the installed helper).

Add, when the trust key changes (D-C60, D-C76): every device needs the installer; the update page shows "Installer nötig" with the reason that the release brings another trusted key.

## Verify a release manually

Needs `minisign` and the public key `release/oarfish-release.pub` from the repository at the release commit.

```bash
mkdir verify && cd verify
gh release download v0.1.0-alpha.1 --repo swingstate/oarfish
minisign -Vm checksums.txt -x checksums.txt.sig -p /path/to/oarfish-release.pub   # 1. signature
sha256sum -c checksums.txt                                                      # 2. hashes (macOS: shasum -a 256 -c checksums.txt)
jq . manifest.json                                                              # 3. metadata
tar -tzvf oarfish_0.1.0-alpha.1_linux_arm64.tar.gz                                # 4. contents
```

The repository script does the same plus consistency checks between manifest, checksums and tarball contents: `scripts/verify-release.sh <dir> release/oarfish-release.pub`.

Order matters: check the signature first, because the hashes are only as trustworthy as the signed `checksums.txt`.

### Signature format

Releases are signed with `minisign -S -l` (legacy mode, algorithm id `Ed`), so the signature is plain Ed25519 over the bytes of `checksums.txt` and can be verified with Go's `crypto/ed25519` without BLAKE2b. `minisign -V` accepts both modes; `scripts/verify-release.sh` additionally rejects anything that is not `Ed`. **The format is fixed once `v0.1.0-alpha.1` is published:** installer and updater will be written against it, so `-l` in `scripts/sign-release.sh` must not be removed afterwards. Rationale and file layout: ADR 0005, Amendment.

## Verify a release anonymously

For a reader without a GitHub account, token or `gh`. The repository is public (D-C17), so the URLs answer without a login. Needs `curl` and `minisign`.

```bash
TAG=v0.1.0-alpha.2
VER=${TAG#v}
mkdir verify && cd verify
base="https://github.com/swingstate/oarfish/releases/download/$TAG"
for f in checksums.txt checksums.txt.sig install.sh manifest.json sbom-backend.cdx.json sbom-frontend.cdx.json \
         "oarfish_${VER}_linux_arm64.tar.gz"; do
  curl -fsSLO "$base/$f"
done
# The public key at the tagged commit (the file the release workflow verifies against).
curl -fsSL -o oarfish-release.pub \
  "https://raw.githubusercontent.com/swingstate/oarfish/$TAG/release/oarfish-release.pub"

minisign -Vm checksums.txt -x checksums.txt.sig -p oarfish-release.pub   # 1. signature
sha256sum -c checksums.txt                                               # 2. hashes (macOS: shasum -a 256 -c checksums.txt)
```

`minisign` prints `Signature and comment signature verified` and the trusted comment `oarfish <tag> <commit>`. Only then are the hashes in `checksums.txt` worth anything. Download every file listed in `checksums.txt` (the loop does), otherwise `sha256sum -c` reports the missing ones.

What this does and does not prove: the signature proves the release files were signed with the private key behind `release/oarfish-release.pub`. The public key comes from the same repository, so it does not protect against someone who controls the repository itself. For that, compare the key id (`11F1C52777090BFC`, the number in the `untrusted comment` line of the key file) and the key line with a copy obtained through an independent channel. No such second channel exists yet; installer and updater will carry the key themselves (Gates G3 and G6).

Status: verified on 2026-10-26 for `v0.1.0-alpha.2`: anonymous `curl` without a token, the signature, the SHA-256 of the files and the version stamp in `install.sh`. Verified again for `v0.1.0-alpha.6` (2026-10-31): token-free download, the signature against `release/oarfish-release.pub`, the SHA-256 of all listed files, `scripts/verify-release.sh` printed `VERIFIED`, and the version stamp and commit in `install.sh` and `manifest.json` matched. Verified again for `v0.1.0-alpha.12`, the most recent release checked (2026-11-13): download with `curl` and without a token (`GH_TOKEN` and `GITHUB_TOKEN` unset), the public key from the tag, `scripts/verify-release.sh` printed `VERIFIED` (signature, the SHA-256 of all listed files, the manifest and the tarball contents), the version stamp in `install.sh` is `0.1.0-alpha.12`, and the commit in `manifest.json` is that of the tag. (`v0.1.0-alpha.1` has no `install.sh`; the loop above does not fit it.) The `releases/latest/download/` address is not covered by this; it is to be verified with the first stable release (see `INSTALL.md`).

## Build a bundle locally (unsigned)

```bash
make release-bundle        # frontend build + scripts/release-bundle.sh -> dist/release/
```

This is the same script CI uses. `VERSION=<semver>` and `COMMIT=<sha>` can be set; the default version is `0.0.0-dev.<short sha>` (plus `.dirty` with local changes). The bundle is only reproducible for the same commit, the same version string and the same Go toolchain (`backend/go.mod`).

## Reproducibility

The bundle is a pure function of commit, version and toolchain:

- Go: `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, `-mod=readonly`, version and commit via `-ldflags`.
- Tar and gzip are written by `scripts/bundlegen` (Go standard library), not by the platform `tar`: entries sorted, mtime = commit timestamp, owner 0:0 numeric without names, gzip header without name or timestamp, fixed compression level. `build_date` in the manifest is the commit timestamp.

Check it:

```bash
make check-reproducible    # rebuilds the frontend and the bundle twice, empty Go cache each time
```

`scripts/check-reproducible.sh` compares the SHA-256 of both bundles and the manifests, runs `scripts/check-helper-independent.sh` and builds a third bundle with another version, whose helper, helper units and `oarfish.service` must equal those of the first (D-C80). CI runs it on every change (job "Reproducible bundle"), and the release workflow additionally compares two rebuilds with the bundle it is about to publish. Not covered: a different Go version or Node version can change the output (the Go version is pinned through `go.mod`, Node through `frontend/.node-version` and the lockfile); SBOM files contain timestamps and UUIDs and are not reproducible.

## How `install.sh` is built and checked

`release/oarfish-release.pub` is the single source of the trusted key. Gate G3 embeds its key line and key id as constants in `install/install.sh` (ADR 0006); Gate G6 will embed the file in the Oarfish binary with `go:embed`. Both must verify `checksums.txt.sig` before trusting any hash. The updater can use `crypto/ed25519` from the standard library (legacy mode). See ADR 0005 for the open point (key rotation).

- `scripts/release-bundle.sh` calls `scripts/stamp-installer.sh`, which copies `install/install.sh` and replaces the single line `OARFISH_STAMPED_VERSION='@@OARFISH_VERSION@@'` with the release version. No timestamps, no host data: the result is a pure function of the file and the version, and `check-reproducible.sh` compares it across two builds. An installer taken from the release installs that release; `--version` overrides.
- `bundlegen check` (part of `verify-release.sh`, run in the dry run, in `sign` and again in `publish`) requires `install.sh` in `checksums.txt`, a `#!/bin/sh` first line, no remaining placeholder and exactly one stamp line equal to the manifest's version. A tampered installer fails the hash, a replaced one fails the signature, a wrongly stamped one fails the check; `scripts/test-signing.sh` tests all three.
- `scripts/check-installer-key.sh` (CI, and in the `sign` job against the file being released) requires the embedded key and key id to equal `release/oarfish-release.pub`. If the key ever changes, change both and release a new installer.
- `scripts/check-budgets.sh` keeps `install.sh` under 80,000 bytes (D-C89 item 1; it was 50,000). G7.3b moved it from 50,244 to 54,304 bytes.
- The installer tests use a copy with a throwaway key (`scripts/make-test-installer.sh`). Such copies are never part of a release.

## Before making the repository public

Decision D-C17 makes the repository public after Gate G2 and this review, before Gate G3. Gate G2.1 did the review; the visibility change is a manual step by the maintainer. Run through this list again right before flipping the switch, because history and workflows may have changed since.

- [ ] **Secret scan, full history and tree.** `gitleaks git --redact --log-opts="--all" .` and `gitleaks dir --redact .` report no leaks. `--redact` keeps values out of the output.
- [ ] **Authors.** `git log --all --format='%an <%ae>%n%cn <%ce>' | sort -u` lists only GitHub noreply addresses (plus `GitHub <noreply@github.com>` for web merges and the `Co-Authored-By` trailer address).
- [ ] **Machine-specific values.** No hits for home directories, local user or host names, private IPs or e-mail addresses in `git grep -I -n -E '<pattern>' $(git rev-list --all)` (zsh: use an array, the list is not word-split).
- [ ] **No private key, `.env` or build artifact ever committed.** `git log --all --diff-filter=A --name-only --format= | sort -u` shows none. Only `release/oarfish-release.pub` and the public test fixtures under `scripts/bundlegen/testdata/` carry minisign material.
- [ ] **Workflows.** No `pull_request_target`, no `workflow_run`, no untrusted `github.event` field in a shell, every third-party action pinned to a full commit SHA, minimal `permissions`, `OARFISH_RELEASE_SIGNING_KEY` referenced in one step of one job that only starts for a tag push.
- [ ] **License.** `LICENSE` is Apache-2.0, `README.md` names it, no manifest claims another one, no dependency with an incompatible license.
- [ ] **GitHub settings** (a) to (f) in `docs/PUBLICATION_CHECKLIST.md`.
- [ ] **Visibility change to public** (step (g) there), then the anonymous download test (step (h)).

Rewriting history is not part of this list. If a scan ever finds a real secret in history, rotate it first (for the release key: new key pair, new public key, and the consequences in ADR 0005), then decide about a rewrite.

## Known limitations

- The installer is described in `INSTALL.md`, including what has and has not been verified. It has run on one Raspberry Pi Zero 2 W (single data point, see the hardware test results there); nothing else has run on real hardware. The components shape was installed once on one device with a test release and a test copy of the installer (device observation, G6.3 owner stop, 2026-11-03), so the installer's rollback after a failed health check, `--purge` and a first installation without a previous release have not run on a device. The release-key path of the installer for that shape ran later, in the G6.9 run, with the official installers of `v0.1.0-alpha.7`, `v0.1.0-alpha.8` and `v0.1.0-alpha.10` (class D, observed on one device; [`UPDATE.md`](UPDATE.md#results-on-the-reference-device)).
- `v0.1.0-alpha.1` was published before the installer existed. It has no `install.sh`, no `oarfish.service` and no `THIRD_PARTY_NOTICES.txt` in the bundle, so the current `verify-release.sh` and the installer reject it by design. Releases from `v0.1.0-alpha.2` on follow the layout described here.

- Unverified until the first real release: the publish job, `gh release create`, prerelease flag, and the secret-based signing path. The dry run exercises everything except those and the committed public key.
- The ARM64 smoke test runs under qemu-user on an x86-64 runner. It proves the binary starts and serves; it is not a test on real hardware (the hardware tests, G3, G4, G5, G5.1 and the G6.3 owner stop, are recorded in `INSTALL.md`).
- No SLSA provenance or artifact attestation, no signature on the SBOMs other than through `checksums.txt`.
- Key rotation and revocation are not designed (ADR 0005).
