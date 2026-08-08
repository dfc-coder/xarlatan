#!/usr/bin/env python3
"""Persistent OpenVINO voice inference worker for Xarlatan.

Control metadata is newline-delimited JSON. PCM payloads are raw little-endian
float32 bytes immediately following the header. This keeps high-frequency audio
out of the generic JSON control plane.
"""

from __future__ import annotations

import argparse
import json
import os
import struct
import sys
from dataclasses import dataclass
from typing import Any, BinaryIO


@dataclass
class DeviceState:
    requested: str
    resolved: str
    fallback_reason: str = ""


class FakeEngine:
    def __init__(self, mode: str, requested: str) -> None:
        resolved = os.environ.get("XARLATAN_FAKE_RESOLVED_DEVICE", requested)
        reason = "" if resolved == requested else f"{requested} unavailable; using {resolved}"
        self.mode = mode
        self.device = DeviceState(requested, resolved, reason)

    def transcribe(self, _samples: bytes) -> str:
        return "transcripción de prueba"

    def synthesize(self, _text: str) -> tuple[bytes, int]:
        return struct.pack("<fff", 0.1, -0.2, 0.3), 24000


class OpenVINOEngine:
    def __init__(self, args: argparse.Namespace) -> None:
        import numpy as np
        import openvino as ov
        import openvino_genai

        self.np = np
        self.ov = ov
        self.ov_genai = openvino_genai
        self.args = args
        self.mode = args.mode
        self.device = DeviceState(args.device, args.device, "")
        self.pipeline: Any = None
        self.generation_config: Any = None
        self.speaker_embedding: Any = None
        self._initialize_with_fallback()

    def _initialize_with_fallback(self) -> None:
        try:
            self._initialize(self.args.device)
            self.device = DeviceState(self.args.device, self.args.device, "")
        except Exception as preferred_error:
            fallback = self.args.fallback_device
            if not fallback or fallback == self.args.device:
                raise
            self._initialize(fallback)
            self.device = DeviceState(
                self.args.device,
                fallback,
                f"{self.args.device} unavailable: {preferred_error}; using {fallback}",
            )

    def _properties(self, device: str) -> dict[str, Any]:
        properties: dict[str, Any] = {}
        if self.args.cache_dir and ("GPU" in device or device == "NPU"):
            os.makedirs(self.args.cache_dir, exist_ok=True)
            properties["CACHE_DIR"] = self.args.cache_dir
        return properties

    def _initialize(self, device: str) -> None:
        if not self.args.model_dir:
            raise RuntimeError("--model-dir is required outside fake mode")
        properties = self._properties(device)
        if self.mode == "stt":
            self.pipeline = self.ov_genai.ASRPipeline(self.args.model_dir, device, **properties)
            config = self.pipeline.get_generation_config()
            language = self.args.language.strip().lower()
            if language and language != "auto":
                config.language = f"<|{language}|>"
            config.task = "transcribe"
            self.generation_config = config
            return

        self.pipeline = self.ov_genai.Text2SpeechPipeline(self.args.model_dir, device, **properties)
        if not self.args.voice_file:
            raise RuntimeError("--voice-file is required for Kokoro")
        shape = self.pipeline.get_speaker_embedding_shape()
        data = self.np.fromfile(self.args.voice_file, dtype=self.np.float32)
        if data.size == 0:
            raise RuntimeError(f"speaker embedding is empty: {self.args.voice_file}")
        self.speaker_embedding = self.ov.Tensor(data.reshape(shape))

    def transcribe(self, payload: bytes) -> str:
        if self.mode != "stt":
            raise RuntimeError("transcribe sent to TTS worker")
        if len(payload) % 4:
            raise RuntimeError("STT PCM payload is not float32-aligned")
        samples = self.np.frombuffer(payload, dtype="<f4").astype(self.np.float32, copy=False)
        result = self.pipeline.generate(samples.tolist(), self.generation_config)
        texts = list(result.texts)
        return texts[0].strip() if texts else ""

    def synthesize(self, text: str) -> tuple[bytes, int]:
        if self.mode != "tts":
            raise RuntimeError("synthesize sent to STT worker")
        properties: dict[str, Any] = {}
        language = self.args.language.strip().lower()
        if language:
            properties["language"] = language
        result = self.pipeline.generate(text, self.speaker_embedding, **properties)
        if len(result.speeches) != 1:
            raise RuntimeError("Kokoro returned an unexpected number of waveforms")
        speech = self.np.array(result.speeches[0].data, dtype=self.np.float32).reshape(-1)
        little_endian = speech.astype("<f4", copy=False)
        return little_endian.tobytes(), int(result.output_sample_rate)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=("stt", "tts"), required=True)
    parser.add_argument("--model-dir", default="")
    parser.add_argument("--voice-file", default="")
    parser.add_argument("--language", default="es")
    parser.add_argument("--device", default="CPU")
    parser.add_argument("--fallback-device", default="CPU")
    parser.add_argument("--cache-dir", default="")
    return parser.parse_args()


def read_exact(stream: BinaryIO, size: int) -> bytes:
    data = bytearray()
    while len(data) < size:
        chunk = stream.read(size - len(data))
        if not chunk:
            raise EOFError("unexpected EOF while reading payload")
        data.extend(chunk)
    return bytes(data)


def write_frame(stream: BinaryIO, header: dict[str, Any], payload: bytes = b"") -> None:
    header["payload_bytes"] = len(payload)
    stream.write((json.dumps(header, separators=(",", ":")) + "\n").encode("utf-8"))
    if payload:
        stream.write(payload)
    stream.flush()


def response_base(request_id: int, engine: Any) -> dict[str, Any]:
    return {
        "id": request_id,
        "ok": True,
        "requested_device": engine.device.requested,
        "resolved_device": engine.device.resolved,
        "fallback_reason": engine.device.fallback_reason,
    }


def serve(engine: Any, reader: BinaryIO, writer: BinaryIO) -> None:
    while True:
        line = reader.readline()
        if not line:
            return
        request: dict[str, Any] = {}
        try:
            request = json.loads(line)
            request_id = int(request["id"])
            payload_size = int(request.get("payload_bytes", 0))
            if payload_size < 0:
                raise RuntimeError("negative payload length")
            payload = read_exact(reader, payload_size) if payload_size else b""
            op = request.get("op")
            response = response_base(request_id, engine)
            output = b""
            if op == "health":
                pass
            elif op == "transcribe":
                if int(request.get("sample_rate", 0)) != 16000 or int(request.get("channels", 0)) != 1:
                    raise RuntimeError("STT requires mono 16 kHz float32 PCM")
                response["text"] = engine.transcribe(payload)
            elif op == "synthesize":
                output, sample_rate = engine.synthesize(str(request.get("text", "")))
                response["sample_rate"] = sample_rate
                response["channels"] = 1
            else:
                raise RuntimeError(f"unknown worker operation: {op}")
            write_frame(writer, response, output)
        except Exception as exc:
            request_id = request.get("id", 0) if isinstance(request, dict) else 0
            write_frame(writer, {"id": request_id, "ok": False, "error": str(exc)})


def main() -> int:
    args = parse_args()
    if os.environ.get("XARLATAN_VOICE_WORKER_FAKE") == "1":
        engine: Any = FakeEngine(args.mode, args.device)
    else:
        engine = OpenVINOEngine(args)
    serve(engine, sys.stdin.buffer, sys.stdout.buffer)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
