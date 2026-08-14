# Protobuf sources

`danmaku.proto` is the minimal schema required to decode `x/v2/dm/web/seg.so`.
Generated Go code is committed under `internal/bili/danmaku/pb`; production builds
do not run `protoc`. To regenerate during development with protoc 31.1 and
`protoc-gen-go` 1.36.10, run from the repository root:

```bash
protoc --go_out=. --go_opt=module=github.com/FortyTwoCn/cyber-amber proto/danmaku.proto
```

Generator versions and licenses are recorded in `THIRD_PARTY_NOTICES.md`.
