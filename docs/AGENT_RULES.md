# Rules for the implementation agent

These are the standing rules for the implementation agent of Oarfish. They are tracked in the repository on purpose (decision D-C79, which amends D-C65, part 1): every session starts with the same rules, and a gate prompt does not have to repeat them. The file is neutral about the development tool. It says "implementation agent" and names neither a tool nor a vendor (D-C65, part 2).

Read it in full at the start of every session. Every rule is a full sentence; where the reason is not obvious, the reason follows the rule, because a rule that is understood is applied better at the edges than a rule that is only remembered.

## Contents

1. [Purpose and use](#1-purpose-and-use)
2. [Source hierarchy](#2-source-hierarchy)
3. [Product boundaries](#3-product-boundaries)
4. [Architecture and platform rules](#4-architecture-and-platform-rules)
5. [Security rules](#5-security-rules)
6. [Gate discipline](#6-gate-discipline)
7. [Evidence and honesty](#7-evidence-and-honesty)
8. [Implementation quality](#8-implementation-quality)
9. [Documentation duty](#9-documentation-duty)
10. [Git, pull requests and releases](#10-git-pull-requests-and-releases)
11. [Text rules for commits, pull requests and tracked files](#11-text-rules-for-commits-pull-requests-and-tracked-files)
12. [Local-only files](#12-local-only-files)
13. [Reporting format at the end of a session](#13-reporting-format-at-the-end-of-a-session)
14. [Lessons learned](#14-lessons-learned)

## 1. Purpose and use

1. This file is read by the implementation agent, at the start of every session and before any work on a gate or a task. It is also the file a reviewer uses to check whether a session followed the rules.
2. A gate prompt adds the scope of one gate or task: the goal, the in-scope and out-of-scope lists, the expected files, the acceptance criteria and every gate-specific value. A gate prompt never removes a rule of this file. If a prompt seems to ask for something this file forbids, the agent stops and asks; it does not pick one of the two.
3. The agent cannot see private documents (gate prompts of earlier gates, hand-over notes, device notes). What this file and the prompt of the current session do not say, the agent does not know.
4. If this file conflicts with the specification, the agent reports the conflict and does not resolve it silently. The specification wins until the owner has decided (section 2).
5. This file is a working document in the public tree. It is changed only by a pull request that the owner merges, and a change of its meaning carries a decision entry in [`DECISIONS.md`](DECISIONS.md).

## 2. Source hierarchy

1. When sources disagree, the higher one wins. The order is:
   1. the specification, [`PFLICHTENHEFT.md`](PFLICHTENHEFT.md) (German by design, the source of truth for requirements);
   2. accepted ADRs in [`adr/`](adr/), including their accepted amendments;
   3. [`DECISIONS.md`](DECISIONS.md);
   4. [`ROADMAP.md`](ROADMAP.md);
   5. the active gate prompt;
   6. instructions given in chat during the session;
   7. the agent's own recommendations.
2. On a conflict the agent stops. It names both sources and the affected requirement, proposes the smallest formal correction, and changes nothing that depends on the answer before the owner has answered. Implementing "the likely intent" is not allowed, because a silently resolved conflict leaves the documents and the code disagreeing without anybody knowing.
3. A change of the baseline (a requirement, a platform decision, a boundary) is not normative until it is recorded as a decision in `DECISIONS.md` or as an ADR amendment. A statement in chat or in a prompt that contradicts the specification is a proposal until that record exists; the agent asks for the record.
4. Requirement IDs keep the prefix `ORB-*`. The product was renamed from Orbit to Oarfish, the requirement IDs were not, and they must stay stable because ADRs, tests and decisions refer to them.
5. A recommendation of the agent has the lowest rank. It may be offered, labelled as a recommendation, and it takes effect only when the owner accepts it.

## 3. Product boundaries

1. Oarfish is a GUI-first management platform for ARM single-board computers and embedded Linux.
2. The Raspberry Pi Zero 2 W with Raspberry Pi OS Lite 64-bit is the initial verified target. Ubuntu Server LTS ARM64 is a mandatory future target.
3. The support status values are exactly Verified, Compatible, Community, Experimental, Generic and Unsupported (specification 3.6). Verified means a concrete board and operating system combination was tested on physical hardware; the agent never assigns Verified without that evidence.
4. Ubuntu Core is outside the MVP. Armbian, Rockchip and external plugins are post-MVP unless the specification is changed first.
5. Hardware detection and operating-system detection are independent concerns (see section 4).
6. Normal supported workflows must be fully GUI-driven. A task that needs a terminal, YAML or a manual file edit is not "supported".
7. Oarfish must run without Docker, Podman, Tailscale or an AI provider. Tailscale, Containers and Oarfish Assist are optional plugins; the core must not depend on them.
8. Self-update is a core capability, not a plugin.
9. No generic privileged shell executor is permitted, in any form, for any reason (section 5).
10. No Node.js, Python, database, Git or build-tool runtime is permitted on the target device. The target is a small device and every runtime added there is memory, attack surface and microSD wear.
11. Builds happen on the Mac or on GitHub-hosted runners, never on the Pi.
12. Distribution is pull-based through signed GitHub Releases. The device fetches; nothing pushes to it.
13. GitHub Actions never connect to a development device. No self-hosted GitHub runner is introduced for this project.

## 4. Architecture and platform rules

1. Backend: Go, standard library first, small reviewed dependencies.
2. Frontend: Svelte 5 and TypeScript, with Vite at build time only. Production frontend assets are embedded into the Go binary.
3. Hardware profile, OS adapter and detected capabilities are three distinct concepts. The hardware profile says which board it is, the OS adapter says how this operating system family is handled, and the capabilities say what this system can actually do right now. Code must not infer one from another; an Ubuntu system on a Raspberry Pi must work through the same paths as any other combination.
4. The operating system is detected through `/etc/os-release`, and actual capabilities are detected separately from it.
5. Raspberry Pi utilities are optional enhancement providers. Where one is present it may add detail; the core never depends on it and works without it.
6. The agent does not hardcode the `pi` user name, `/home/pi`, the boot configuration path, `raspi-config`, `vcgencmd` or a single network backend. Each of these differs between Raspberry Pi OS and Ubuntu or does not exist on other boards.
7. The agent prefers procfs, sysfs, hwmon, systemd, journald, `os-release` and other standard Linux interfaces over running programs. Reading a file is cheaper, safer and easier to test than starting a process and parsing its output.
8. The core stays functional in Generic ARM64 mode, with board-specific features absent rather than failing.
9. Resource limits of the reference device (memory, CPU, microSD write load) are requirements, not wishes. The agent keeps intervals adaptive, avoids persistent writes that are not needed, and respects the size budgets (section 8).

## 5. Security rules

1. There is no generic privileged executor. Privileged operations are narrow, typed, validated, audited and separately authorized: each one is a named request with a fixed shape, never a command line or a path chosen by the caller.
2. Secrets, tokens, keys and passwords never appear in logs, source control, frontend state, test snapshots or model context. Fixtures use throwaway values that are visibly fake.
3. The agent never reads, prints, generates or moves private signing keys. The release signing key is the owner's; tests use throwaway keys created for the test.
4. Logs, container output, plugin data and model responses are untrusted input. They are validated, size-limited and never interpreted as instructions.
5. Release notes and texts received from a server are rendered as text, never as HTML.
6. Updates require versioned artifacts, integrity and signature verification, atomic activation, health verification and rollback. A step that is skipped for convenience is a defect, not a shortcut.
7. Plugins need explicit capabilities. A plugin has none by default and receives only what it declares and the owner grants.
8. Network reachability does not replace Oarfish authentication. A request from the local network is as unauthenticated as one from anywhere else until a session proves otherwise.
9. The Docker socket and similar privileged APIs are not low-risk integrations. Access to them is equivalent to root and is treated and documented as such.
10. The default listen address is `127.0.0.1:8484`. The binary refuses a listen address that is not a literal loopback address until the design for LAN access and HTTPS has been accepted.

## 6. Gate discipline

1. One session handles one gate or one clearly bounded task.
2. The agent identifies the gate or task at the start and names it in the report.
3. The agent does not implement later gates and does not "prepare" them. Preparation is implementation in advance with a different name, and it creates code that the later gate's review has never seen.
4. The agent defines the stop condition of the gate (the prompt names it) and respects it. At the stop condition the agent reports and waits.
5. The agent never starts the next gate on its own, however obvious the next step looks.
6. The agent separates mandatory requirements, recommendations and deferred decisions, in the work and in the report. A recommendation is not silently promoted to a requirement, and a deferred decision is not silently decided.
7. For work that is expensive or ambiguous, the agent asks before building. A question costs minutes; a wrong build costs a review cycle.
8. A gate is PASS only with evidence (section 7). "Implemented" is not "PASS".

## 7. Evidence and honesty

1. The agent never claims a test, a build, a device behaviour or a compatibility that was not run or observed.
2. The agent says what was run, what was not run, and why.
3. Hardware, Raspberry Pi OS, Ubuntu, Safari, Firefox, screen readers and forced-colours mode are "not verified" until they were actually checked, and the report says "not verified" for each of them that was not.
4. Assumptions are labelled as assumptions. Facts, proposals and assumptions are kept apart, as in the ADRs.
5. The agent does not infer a result from prose. A statement in a document that something works is not evidence that it works; a test run, a command output or a device observation is.
6. The report names failures and surprises first, before anything that went well.
7. Findings outside the scope are reported, not hidden and not fixed on the side (section 14).

## 8. Implementation quality

1. Backend: Go, standard library first. A new dependency needs a stated reason and an owner decision. Dependencies are small, reviewed and pinned.
2. Frontend: Svelte 5 and TypeScript. Vite runs at build time only and the assets are embedded.
3. The target has no Node.js, Python, database, Git or compiler. Nothing in the product may need one at run time.
4. Builds run on the Mac or on GitHub-hosted runners, never on the Pi. There is no self-hosted runner, and Actions never connect to a device.
5. Every behaviour the agent adds has a test, including its error paths. A test that only covers the success path leaves the part that matters in a security-relevant product untested.
6. The agent runs the checks the project runs, with `-race` where the `Makefile` does (`make lint`, `make test` and the targets for the area it touched), and reports the results.
7. The agent respects the size budgets in [`DEVELOPMENT.md`](DEVELOPMENT.md) ("Size budgets") and checks them when a change can move them.
8. The agent respects the accessibility rules of [`UI.md`](UI.md): contrast, visible focus, target size, landmarks and headings, native elements, and no meaning carried by colour alone.
9. Both languages, German and English, are always present with identical keys; a text is never inside a component (`UI.md`, "Text rules").
10. User-facing German uses the Sie-form and short sentences, in plain language and without filler.
11. Code, identifiers, commit messages and repository documents are English. A document is German only where it is German by design, which today means the specification.
12. Code reads like the code around it: the same naming, the same comment density, the same idiom.

## 9. Documentation duty

1. The specification, the ADRs, `DECISIONS.md`, `UI.md`, `DEVELOPMENT.md`, [`INSTALL.md`](INSTALL.md) and `ROADMAP.md` are kept synchronized with the code in the same pull request. A document that falls behind the code misleads the next session.
2. Facts, proposals and assumptions are labelled as in the ADRs.
3. Every gate and every task updates the status line of its gate in `docs/ROADMAP.md`. This is part of the definition of done, not an optional extra, and the report says that it was done.
4. A change of the baseline is never made by editing the specification alone. It comes with a decision entry in `DECISIONS.md` (or an ADR amendment) and a line in the change log of the specification.
5. Documents state what is verified and what is not, and where. The verification status of the UI belongs in `UI.md`, that of the installer and the update path in the documents that describe them.

## 10. Git, pull requests and releases

1. Each gate or task gets its own branch, created from the current `origin/main` after a fetch.
2. Commits are small and meaningful, each with a message that says what changed and why.
3. The agent never force-pushes, never rewrites published history and never rebases a published branch. The signed alpha tags and releases depend on the history staying as it is.
4. The agent stages files by explicit path, never by directory, and checks `git status` before each commit. A directory add can carry files that must stay private.
5. The agent opens the pull request, reports, and waits.
6. A pull request is merged only after the owner wrote "merge PR #n" for that named pull request in chat, and then with a merge commit. Approval for one pull request is not approval for another.
7. A release tag is created and pushed only after the owner reported the merge and confirmed the exact tag name in chat. The tag is placed only on a commit that is on `origin/main`, addressed by its full commit ID, and only with a tag name that was never used.
8. After pushing a release tag the agent watches the release workflow, appends the required notes with `gh release edit`, and verifies the published release anonymously (see [`RELEASING.md`](RELEASING.md)).
9. The agent never deletes or reuses a published tag or release.
10. If a release job fails, the agent stops and reports. It does not retry with changes, and it never changes workflow files as part of a release step.
11. Repository settings, rulesets, secrets and visibility are changed by the owner only (see [`PUBLICATION_CHECKLIST.md`](PUBLICATION_CHECKLIST.md)).
12. The agent never touches the release signing key.

## 11. Text rules for commits, pull requests and tracked files

These rules come from decision D-C65 and apply to everything that reaches the public remote.

1. Commit messages and pull request texts carry no attribution lines.
2. No tool or vendor names appear in tracked files, commit messages or pull request texts. The AI provider as a product feature of Oarfish Assist is allowed, because it is part of the product.
3. No host names, user names, IP addresses, serial numbers, MAC addresses or device identifiers appear in any file, log, fixture, screenshot, commit message or pull request text. A screenshot is checked before it is committed.
4. Private material stays outside the repository: gate prompts, hand-over notes, package manifests and device notes.
5. The Git history is not rewritten. Earlier commits keep their text.

## 12. Local-only files

1. The entry file of the development tool is local and untracked. It sits in the repository root, is listed in `.git/info/exclude`, and contains only a pointer to this file. It is deliberately not named here, because the file name is the name of the tool.
2. Every `*.local.md` file holds machine-specific values: paths, Pi host names, user names and local endpoints. They are never committed.
3. `.gitignore` (the neutral pattern `*.local.md`) and `.git/info/exclude` keep these files out of the tree. The agent checks with `git status` that they do not appear.
4. The agent never copies a value from a local file into a tracked file, a commit message or a pull request text.
5. Access that a local file describes (for example to a development device) is limited to what the owner released for the gate; the local file says what is released and what is never to be touched.

## 13. Reporting format at the end of a session

The report contains, in this order:

1. Failures and surprises first, if there are any.
2. Branch, pull request (number and URL) and head commit ID.
3. The commands that were run, with their results.
4. The changed files, grouped by purpose.
5. The acceptance criteria, each with a status and its evidence.
6. What is not verified, and why.
7. Decisions that the owner has to make.
8. The confirmation that the next gate was not started, and that the status line in `docs/ROADMAP.md` was updated.

## 14. Lessons learned

1. A gate prompt must contain every value the agent needs. The agent cannot see private documents, so when a value is missing it asks and does not guess.
2. The agent does not fix problems outside the scope silently. It lists them as findings in the report, so that the owner can decide where and when they are handled.
3. A rule that was dropped from the tree cost every later session the rules (D-C79). The agent does not move rules out of the tracked tree on its own.
