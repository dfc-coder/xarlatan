# v0.7.0 Release Evidence

Issue: #68
Delivery PR: #69
Branch: `delivery/v0.7.0`

## Requirement / SDD

The authoritative design is `V07_AGENT_AGNOSTIC_ACP_SPEC.md`.

The release boundary is:

```text
Xarlatan voice plane -> generic ACP v1 -> external agent runtime
```

ZeroClaw and NullClaw are compatibility profiles, not packages imported by the
Xarlatan core.

## TDD RED

Valid RED commit:

```text
b48923a4d4d8b95d41c5c6cf0f16f262a071c615
```

GitHub Actions CI run **#860** reached the intended behavioral tests only after
both formatting and vet passed.

The two intended failures were:

```text
TestV07ACPUsesPortablePromptContentBlocks
session/prompt must use an ACP content-block array

TestV07ACPReconstructsNullClawReplyWithoutTerminalContent
Reply = "", want reconstructed streamed reply
```

This proved the v0.6 ZeroClaw-specific wire behavior was insufficient for the
portable v0.7 ACP contract. No formatting, syntax, fixture or infrastructure
failure was accepted as RED evidence.

## Minimal GREEN

The smallest compatibility change was made first in the existing v0.6 ACP
client:

```text
4e6a70f51891c644153a525e506568c9b21962f1
```

It changed `session/prompt.params.prompt` to an ACP text content-block array and
reconstructed a final reply from streamed `agent_message_chunk` text when
terminal `content` was absent.

The subprocess fixture was then updated to accept the portable prompt shape:

```text
08417a05b7a0958bab59bbc94a43ee9b5cd54593
```

CI run **#864** completed successfully, including formatting, vet, full tests,
focused coverage, repeat tests, race tests, build/version and release contracts.

## Refactor

After GREEN, the provider-specific implementation was replaced by:

```text
internal/acp/client.go
internal/acp/responder.go
internal/acp/runtime.go
```

The generic runtime:

- launches configured `binary + args` exactly;
- performs ACP v1 initialization and captures optional `agentInfo`;
- always creates the session with an absolute configured `cwd`;
- sends text content-block prompts;
- streams only user-facing `agent_message_chunk` text;
- never speaks thought/plan/raw tool/permission payloads;
- rejects/cancels permission requests by default;
- accepts ZeroClaw-style terminal `content`;
- reconstructs NullClaw-style responses when terminal `content` is absent;
- sends best-effort `session/cancel` while local cancellation remains
  authoritative;
- drops every late delta from a cancelled turn;
- closes/reaps the owned ACP child idempotently with a bounded kill fallback.

The voice-gateway composition imports `internal/acp` only. The obsolete
`internal/zeroclaw` package is removed.

## Local focused evidence

Before final remote CI, the generic ACP suite was run locally:

```bash
gofmt -w internal/acp/*.go
make format-check
go test -count=1 ./internal/acp
go test -count=1 -coverprofile=/tmp/acp.out ./internal/acp
go tool cover -func=/tmp/acp.out
```

Observed local generic ACP statement coverage:

```text
80.8%
```

This meets the v0.7 floor of 80%.

## RDD / product release surface

v0.7 includes:

- portfolio-first README headline and positioning;
- architecture diagram with ZeroClaw and NullClaw as ACP examples;
- configuration-only agent switching;
- `docs/BETA_V07_RUNBOOK.md`;
- `scripts/tests/v07_contract_test.sh`;
- `scripts/beta_v07_acceptance.sh`;
- v0.7 changelog;
- release packaging for the v0.7 acceptance/runbook;
- physical acceptance with exact `Final result: PASS` gate.

`setup_voice_runtime.sh` contains only voice inference setup and does not install
or configure either agent runtime.

## Security invariants

Only:

```text
session/update
  update.sessionUpdate == agent_message_chunk
  update.content.type == text
```

is speech-eligible.

ACP permission requests are never auto-approved. Logs/latency metadata do not
need transcript content, raw tool payloads, credentials or PCM.

The systemd service keeps the v0.6 hardening and only adds initial writable home
exceptions for `~/.zeroclaw` and `~/.nullclaw` so the two supported external
runtimes can persist their own state.

## Known limitation

Current NullClaw stdio execution performs the agent invocation synchronously.
A local Xarlatan barge-in therefore stops playback/TTS immediately and rejects
late output, but NullClaw may not process `session/cancel` until that invocation
returns. The ACP session can remain busy until then. v0.7 does not claim remote
compute cancellation where the server cannot provide it.

## Rollback

Code rollback point:

```text
v0.6.0-beta.1
3e208eb2691c7e3f6253d6ca0b9eaaa97b54ac0b
```

Operational rollback:

```bash
sudo make rollback
```

v0.7 does not delete or rewrite `~/.zeroclaw`, `~/.nullclaw` or external agent
credentials.

## Final automated GREEN

Validated implementation head:

```text
ef6bc8b9ca1c5b2cd14ba05d3830bd87fe767e36
```

GitHub Actions evidence on that exact implementation head:

```text
baseline inventory: run #404 — PASS
CI:                 run #886 — PASS
make format-check:                  PASS
go vet ./...:                       PASS
go test -count=1 ./...:             PASS
generic ACP statement coverage:    81.1% (floor 80%)
focused repeat x20:                 PASS
focused race suite:                 PASS
Python voice-worker compile/syntax: PASS
make clean build:                   PASS
embedded version:                   assistant v0.7.0
bin/llama-server from default build: absent — PASS
WI-08 installation contracts:      PASS
WI-09 contracts:                   PASS
WI-11D release contract:           PASS
v0.7 release contract:             PASS
systemd verify:                    PASS
systemd security:                  4.7 OK
```

The focused remote coverage values were:

```text
memory       86.4%
application  82.2%
conversation 85.9%
audio        60.6%
vad          90.7%
sherpa-vad   77.0%
wake         93.3%
barge        83.9%
inference    71.1%
acp          81.1%
```

The commit that records this section changes release evidence only. Its own
baseline inventory and CI runs are the final merge gate; no runtime claim is
advanced solely by this documentation commit.

## Physical acceptance

Automated merge does not constitute physical voice acceptance. Issue #68 stays
OPEN until a target Linux host generates a report ending exactly:

```text
Final result: PASS
```
