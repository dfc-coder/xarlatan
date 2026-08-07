# Xarlatan v0.5.0-beta.1 — physical beta runbook

## 1. Sync and install

```bash
git checkout main
git pull --ff-only
sudo bash ./scripts/install.sh
systemctl --user daemon-reload
```

Verify the installed binary:

```bash
env -u LD_LIBRARY_PATH /usr/local/bin/xarlatan -version
```

Expected:

```text
assistant v0.5.0-beta.1
```

## 2. Run the physical acceptance

Stop any manually-started Xarlatan instance first, then run:

```bash
EXPECTED_VERSION=v0.5.0-beta.1 \
AUDIO_DEVICE=default \
bash ./scripts/beta_acceptance.sh ./config.yaml
```

The v0.5 dispatcher starts the dedicated continuous-voice protocol automatically.

## 3. Interaction protocol

Follow the prompts exactly:

1. speak normally without `Xarlatan` and verify no response;
2. ask a long wake-qualified question so `[partial]` can appear;
3. during the long spoken answer say `Xarlatan, para`;
4. verify playback stops;
5. say `Xarlatan, dime hola` and verify the next turn works;
6. remain silent after playback and verify there is no self-trigger.

The harness also checks that the same `arecord` PID remains alive across the turns.

## 4. Acceptance artifact

The script writes:

```text
beta-acceptance-<timestamp>.md
```

The release is physically accepted only if the final line is:

```text
Final result: PASS
```

If it is `FAIL`, use the first failing gate as the next debugging target; do not close WI-11D.
