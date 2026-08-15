# Third-party notices

Cyber Amber is distributed under **GNU GPL-3.0-only**. See `LICENSE`.

## Source reference

`FortyTwoCn/downkyicore` commit `b8edb9b12bf214411fca933ebb07457c949f577f`, GPL-3.0. The protocol and algorithm reference scope is documented in `docs/REFERENCES.md`. Copyright remains with its respective authors.

`Chiyang001/BiliGo` commit `c546e3b8be6a1b02e9f065f4ca22f69112fb40cc` was inspected only for observable Bilibili request behavior at the user's request. No license was declared at the inspected revision, so no source code from that repository was copied, translated, modified, or distributed in Cyber Amber. See `docs/REFERENCES.md` for the exact inspection scope.

`bggRGjQaUbCoE/PiliPlus` commit `3a7d4614743cb7289293d6c47e13d96aec544f18`, GPL-3.0, was inspected for the current observable comment-image upload field flow. No Dart source or UI was copied; Cyber Amber's independent Go adaptation and tests remain GPL-3.0-only. See `docs/REFERENCES.md`.

## Go dependencies

Direct dependencies and their upstream licenses (verify again when upgrading):

| Module | Version | License |
|---|---:|---|
| github.com/oklog/ulid/v2 | 2.1.2 | Apache-2.0 |
| github.com/prometheus/client_golang | 1.24.1 | Apache-2.0 |
| github.com/skip2/go-qrcode | 2020-06-17 | MIT |
| golang.org/x/crypto | 0.55.0 | BSD-3-Clause |
| golang.org/x/sys | 0.47.0 | BSD-3-Clause |
| google.golang.org/protobuf | 1.36.12 | BSD-3-Clause |
| gopkg.in/yaml.v3 | 3.0.1 | MIT/Apache-2.0 notices in upstream |
| modernc.org/sqlite | 1.44.3 | BSD-3-Clause; SQLite portions are public domain |

Transitive versions are fixed by `go.sum`; their notices remain in upstream modules. Release tooling should generate an SBOM and re-audit licenses after dependency updates.

## Protobuf generator

`proto/danmaku.proto` was generated with Protocol Buffers compiler 31.1 and `protoc-gen-go` 1.36.10; committed generated code means neither is a production runtime dependency. Protocol Buffers is BSD-3-Clause.

## Runtime packages

- FFmpeg: GPL/LGPL components depending on Debian build configuration. The project invokes the system executable and does not bundle modified FFmpeg source. Debian copyright/source packages provide exact build notices.
- libass: ISC license.
- Fontconfig: permissive fontconfig license.
- Noto CJK fonts: SIL Open Font License 1.1. Installed from Debian `fonts-noto-cjk`; no font binary is stored in this repository.
- Debian base, CA certificates and tzdata retain their package licenses.

Container redistributors must preserve Debian `/usr/share/doc/*/copyright` information or otherwise satisfy the corresponding package license/source-offer obligations.
