# What TU-Fast actually stores, and what that means for this project

**Read 2026-08-04 from TU-Fast's own source** (`src/modules/credentials.ts`,
`src/modules/otp.ts`, `src/manifest.chrome.json` in
<https://github.com/TUfast-TUD/TUfast_TUD>). This is not a criticism of
TU-Fast — it is an open-source tool that documents its own trade-off honestly.
It is here because `internal/scraper/tufast_transplant.go` copies this data,
so the property belongs in this repo's docs.

## The mechanism

TU-Fast stores the ZIH username, password, the **TOTP seed**, and the **Indexed
Secret** in `chrome.storage.local` (a LevelDB directory under
`Local Extension Settings/<extension-id>`). Values are AES-CBC encrypted. The
key is derived like this:

```
key = SHA-256( chrome.system.cpu.getInfo()      // model name, core count, arch, features
             + chrome.runtime.getPlatformInfo() )  // os, arch, nacl_arch
```

**There is no secret in that key** — no user passphrase, no OS keystore, no
server-side component. It is public hardware and OS metadata.

Consequences:

- Any process running as the user can call the same two APIs and decrypt.
- Anyone holding a copy of the disk can rebuild the key offline once they know
  the CPU model and OS — a small search space, not a cryptographic barrier.
- So this is **obfuscation, not encryption**. tu-fast.de/datenschutz saying
  credentials are stored "lokal (verschlüsselt)" sets an expectation the code
  does not meet.

## Why it matters more than a stored password would

1. **Both factors live in one place**, under the same non-secret key. 2FA
   reduces to 1FA. TU-Fast's own `docs/2FA.md` says so plainly: *"bypassing 2FA
   naturally defeats the purpose of 2FA. With this function you trade security
   for convenience."*
2. **A stolen TOTP seed does not expire.** A session cookie does. Whoever reads
   the store once keeps generating valid codes until the token is regenerated,
   silently.
3. **The blast radius is the whole ZIH identity, not OPAL.** TU-Fast's content
   scripts cover Selma, QIS, OWA, Cloudstore, Matrix, several GitLabs and SLUB;
   `host_permissions` is `*://*/`; and one content script sits on
   `selfservice.tu-dresden.de/services/idm/token/create` to capture the seed at
   setup time.

## What this project adds to that

`TransplantTUFastLoginData` copies the LevelDB into a second profile, so the
seed then exists **twice** on disk. Combined with unattended scheduled runs, the
machine holds a fully automatable credential to the maintainer's entire
university identity at all times.

That is a deliberate, accepted trade — unattended sync is the point of the tool
— but it should be a known trade, not a surprise.

## Mitigations that actually apply

- **Full-disk encryption (BitLocker).** Since key derivation is purely local,
  this is the only measure that covers the stolen-disk case.
- **Keep browser profile directories out of cloud sync/backup.** Backing up
  that directory backs up the credentials.
- **Know the revocation path in advance:** change the ZIH password *and*
  regenerate the 2FA token in the Self-Service portal (needs the Coupon-ID
  issued at enrolment). A new token makes any stolen seed worthless.
- **Never set this up on a shared or foreign machine.**

## The connection to the WebDAV question

None of this would be needed if OPAL issued a generated, revocable, read-only
access token — the model Nextcloud calls an app password, and the one current
OpenOlat moved to when it dropped Basic auth from WebDAV in August 2024
(commit `6ae28f0e`, OO-7949; the handler now takes `Digest` and `Bearer`).
With such a token this project would need no browser login, no TOTP seed on
disk, and no transplant step at all. See
[`opal-webdav-student-access.md`](opal-webdav-student-access.md).
