# WI-11C2 release note

`v0.5.0-beta.1` keeps offline Whisper as the authoritative final recognizer and adds one bounded preview decode for sufficiently long utterances.

The preview is observable as `[partial] ...` but cannot activate wake handling, memory, tools or AgentRuntime. The final transcript remains the only authoritative input.
