# Publication checklist

Manual GitHub settings for making `swingstate/oarfish` public (decision D-C17). Nothing here is applied by the repository or by Claude Code; each step is done by the maintainer in the GitHub UI. The review of the repository content (history scan, workflows, license) is documented in `docs/RELEASING.md`, "Before making the repository public".

Verification commands use the `gh` CLI, read-only, logged in as a repository admin.

## Read this first: what GitHub allows while the repository is private

Checked on 2026-10-24 with the account's current plan (no paid plan):

| Setting | Private repository today |
|---|---|
| Default workflow permissions | available, already `read`, `can_approve_pull_request_reviews: false` |
| Fork pull request approval | not available (`422 Fork PR approval is not allowed for private repositories`) |
| Rulesets and branch protection | not available (`403 Upgrade to GitHub Pro or make this repository public`) |
| Secret scanning, push protection, private vulnerability reporting | not available through the API today (`security_and_analysis` is empty, the private vulnerability reporting endpoint answers 404); GitHub documents secret scanning as free for public repositories |

Consequence: steps (a), (c), (d), (e) and (f) can only be set and verified after the visibility change (g), not before it. Do them in one sitting directly after (g). Between (g) and the end of (f) the repository is public without those protections. The exposure in that window is small: the signing key is a secret that fork pull requests never receive, and creating a tag needs write access, which only the maintainer has. But it is a window, and the order below keeps the order requested for the gate (a to h) while marking what moves.

Recommended sequence: **(b) → (g) → (a) → (c) → (d) → (e) → (f) → (h)**. If you prefer to have the protections active before publication, the alternative is a paid plan (GitHub Pro) for the duration; then (a) to (f) can be done in the listed order before (g).

Settings → means the repository's Settings tab: `https://github.com/swingstate/oarfish/settings`.

## (a) Require approval for workflows from fork pull requests

- **Where:** Settings → Actions → General → "Approval for running fork pull request workflows from contributors" (older UI wording: "Fork pull request workflows from outside collaborators").
- **Set:** "Require approval for all external contributors".
- **Why:** `ci.yml` and `release.yml` run on `pull_request`. With approval required, no workflow code from a fork runs until the maintainer has looked at it. Fork runs never get secrets and get a read-only token either way; this stops resource abuse and unreviewed script execution.
- **Verify:**
  ```bash
  gh api repos/swingstate/oarfish/actions/permissions/fork-pr-contributor-approval --jq .approval_policy
  # expected: all_external_contributors
  ```

## (b) Default workflow permissions: read-only

- **Where:** Settings → Actions → General → "Workflow permissions".
- **Set:** "Read repository contents and packages permissions". Leave "Allow GitHub Actions to create and approve pull requests" unchecked.
- **Optional, same page:** "Require actions to be pinned to a full-length commit SHA" (all actions in `.github/workflows/` already are; the setting makes GitHub enforce it for future changes). Check `sha_pinning_required` below.
- **Verify** (already true on 2026-10-24):
  ```bash
  gh api repos/swingstate/oarfish/actions/permissions/workflow
  # expected: {"default_workflow_permissions":"read","can_approve_pull_request_reviews":false}
  gh api repos/swingstate/oarfish/actions/permissions --jq .sha_pinning_required   # true if you enabled the option
  ```

## (c) Branch ruleset for `main`

- **Where:** Settings → Rules → Rulesets → New ruleset → New branch ruleset.
- **Set:**
  - Name: `main`. Enforcement status: Active.
  - Target branches: Add target → Include default branch.
  - Rules: check "Restrict deletions", check "Block force pushes", check "Require status checks to pass" and add the CI jobs below.
  - Do **not** check "Require a pull request before merging" with required approvals: there is a single maintainer and nobody could approve.
  - Bypass list: leave empty. (If you later need to push to `main` directly, add "Repository admin" with mode "Exempt" deliberately rather than loosening the rules.)
- **Required status checks** (the job names in `.github/workflows/ci.yml`, workflow "CI"; confirm the exact names in the status check picker or with `gh pr checks`):
  - `Go checks`
  - `Frontend checks and build`
  - `Release bundle, budgets and ARM64 smoke test`
  - `Reproducible bundle`
  - `Signing and tamper tests`
  - `Dependency scan and SBOM generation`
  - `Installer checks and tests` (added in Gate G3)
  - `Installer in arm64 container (debian:12)` and `Installer in arm64 container (ubuntu:24.04)` (added in Gate G3; these pull container images from Docker Hub and need arm64 emulation on the runner, so they are the most likely to fail for reasons outside the repository; require them only if you accept that)

  Do not require the `Release / ...` checks: that workflow only runs on pull requests that touch release files, so a required check would block every other pull request.
- **Consequence:** changes reach `main` only through a pull request whose checks are green; direct pushes are rejected.
- **Verify:**
  ```bash
  gh api repos/swingstate/oarfish/rulesets --jq '.[] | {name, target, enforcement}'
  gh api repos/swingstate/oarfish/rulesets/<id> --jq '{include: .conditions.ref_name.include, rules: [.rules[].type]}'
  # expected rules: deletion, non_fast_forward, required_status_checks
  git push --force origin HEAD:main   # on a scratch clone, must be rejected; do not run it in the working clone
  ```

## (d) Tag ruleset for `v*`

- **Where:** Settings → Rules → Rulesets → New ruleset → New tag ruleset.
- **Set:**
  - Name: `release tags`. Enforcement status: Active.
  - Target tags: Add target → Include by pattern → `v*`.
  - Rules: check "Restrict deletions" and "Block updates" (the tag rule that stops moving a tag; it appears as "Restrict updates" in some UI versions). Do **not** check "Restrict creations": the maintainer must still be able to push a release tag.
  - Bypass list: empty.
- **Consequence:** a published tag such as `v0.1.0-alpha.1` cannot be deleted or moved. `docs/RELEASING.md` says to delete a failed release's tag and tag again; that now requires temporarily disabling this ruleset, which is the intended friction. A published version is never reused.
- **Verify:**
  ```bash
  gh api repos/swingstate/oarfish/rulesets --jq '.[] | select(.target=="tag") | {name, enforcement}'
  gh api repos/swingstate/oarfish/rulesets/<id> --jq '{include: .conditions.ref_name.include, rules: [.rules[].type]}'
  # expected rules: deletion, update
  ```

## (e) Secret scanning and push protection

- **Where:** Settings → Advanced Security (older UI: "Code security and analysis") → "Secret Protection" / "Secret scanning" → Enable; "Push protection" → Enable.
- **Why:** it is free for public repositories. It blocks pushes that contain recognized secret patterns and alerts on leaked ones. It is not guaranteed to recognize a minisign private key, so it is a second line, not a replacement for keeping the key out of the working tree (`*.key` is git-ignored).
- **Verify:**
  ```bash
  gh api repos/swingstate/oarfish --jq '.security_and_analysis | {secret_scanning: .secret_scanning.status, push_protection: .secret_scanning_push_protection.status}'
  # expected: enabled, enabled
  ```

## (f) Private vulnerability reporting

- **Where:** Settings → Advanced Security (older UI: "Code security and analysis") → "Private vulnerability reporting" → Enable.
- **Why:** gives reporters a private channel (Security tab → "Report a vulnerability") instead of a public issue. There is no `SECURITY.md` yet; adding one is a separate documentation task and not part of this gate.
- **Verify:**
  ```bash
  gh api repos/swingstate/oarfish/private-vulnerability-reporting --jq .enabled
  # expected: true
  ```

## (g) Change visibility to public (last step of the review)

- **Do this only after** `docs/RELEASING.md` "Before making the repository public" is ticked off and the gate's pull request is merged (or the maintainer has decided otherwise). See the note at the top for steps that can only follow it.
- **Where:** Settings → General → Danger Zone → "Change repository visibility" → Make public. GitHub asks for the repository name as confirmation.
- **Consequences:** history, tags, releases, issues, pull requests and Actions logs become world-readable, including everything in the 11 commits and the existing releases. Actions run logs of earlier runs become public too. Gate G2.1 did not read them; GitHub masks registered secrets in logs, and the scripts do not print the key (`scripts/sign-release.sh` writes it only to a mode-0600 temp file). Skim the log of the `v0.1.0-alpha.1` release run before step (g) if you want to be sure. Stars and watchers start from zero. The change can be reverted to private later, but forks and clones made in between remain public copies.
- **Verify:**
  ```bash
  gh repo view swingstate/oarfish --json visibility --jq .visibility    # expected: PUBLIC
  curl -sI https://github.com/swingstate/oarfish | head -1              # expected: HTTP/2 200, no login
  ```
  Then do (a), (c), (d), (e), (f) at once.

## (h) After publication: anonymous download test of `v0.1.0-alpha.1`

Do this **without** the GitHub CLI login and in a clean directory, ideally from a machine or private browser window that has never been logged in to GitHub:

```bash
unset GH_TOKEN GITHUB_TOKEN
mkdir -p "$(mktemp -d)" && cd "$_"
# follow "Verify a release anonymously" in docs/RELEASING.md
```

- **Verify:** every download returns 200 without credentials (the `curl -f` calls do not fail), `minisign` prints `Signature and comment signature verified`, `sha256sum -c checksums.txt` lists `OK` for the bundle, manifest and both SBOMs.
- Also check in a logged-out browser: the repository page loads, the Releases page shows `v0.1.0-alpha.1` as pre-release with six assets, and the sidebar shows "Apache-2.0 license". (The GitHub API already reports `Apache-2.0` for the private repository.)
- If a download fails with 404, the repository is still private or the asset name differs; do not paste a token into any installer or documentation as a workaround (D-C13).
