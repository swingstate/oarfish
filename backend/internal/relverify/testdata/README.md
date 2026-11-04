# Test data of the release verification package

## v0.1.0-alpha.6 (golden files)

Three files of the published release `v0.1.0-alpha.6`, signed by the real
release signing job. They are public release artifacts. They were downloaded
anonymously, without a token, from the public release page:

| File | SHA-256 | Source |
|---|---|---|
| `checksums.txt` | `8c527a9edb86d554e9e9273836244601b3ca1dbfd3650cb0249968ae53a91014` | <https://github.com/swingstate/oarfish/releases/download/v0.1.0-alpha.6/checksums.txt> |
| `checksums.txt.sig` | `def20de9b9a6b696d28819bf742775ab03b22950d078135a5f4600da6b41e653` | <https://github.com/swingstate/oarfish/releases/download/v0.1.0-alpha.6/checksums.txt.sig> |
| `manifest.json` | `1e356d9d92d3b965a374c24578d6eb33e025b0b2cc4449dd4d044728b9d4a276` | <https://github.com/swingstate/oarfish/releases/download/v0.1.0-alpha.6/manifest.json> |

The bundle `oarfish_0.1.0-alpha.6_linux_arm64.tar.gz` is not committed. The
golden test checks the signature with the embedded release key
(`trustkey.Release()`), the listed hashes, and the manifest with the rules of
the legacy shape, and that the listed hash of the manifest equals the SHA-256
of the committed `manifest.json`.

`TestGoldenFilesMatchRecordedHashes` fails if a file here no longer has the hash
in this table, so an edited copy cannot pass for the published file.

Nothing else in the package's tests uses the release key. Every other key is a
throwaway key that the tests derive from a fixed label.

## prehashed (a real minisign signature in the default, prehashed format)

`prehashed/message.txt.minisig` is a signature made by `minisign -S` in its default
mode (algorithm `ED`, a BLAKE2b-512 prehash) over `prehashed/message.txt`, with the
throwaway key whose public half is `prehashed/throwaway.pub`. The secret key was
generated for this purpose and deleted at once; it exists nowhere. `minisign -V`
accepts the signature, so it is valid for minisign itself and must still be refused
by `VerifySignature`, which accepts the legacy format `Ed` only (ADR 0005). The
test also runs `minisign -V` when the tool is installed.
