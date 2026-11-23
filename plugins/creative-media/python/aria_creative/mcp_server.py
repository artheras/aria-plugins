"""Stdio MCP server exposing creative & media tools (Figma, Canva, OpenAI Images, Kling, Runway)."""
from __future__ import annotations

import json
import sys
from typing import Any, Dict

from . import (
    canva_client,
    figma_client,
    kling_video_client,
    openai_image_client,
    runway_video_client,
)

TOOLS = [
    {
        "name": "creative_figma_summary",
        "description": "Read-only Figma file summary by file key or URL.",
        "inputSchema": {
            "type": "object",
            "properties": {"file_key": {"type": "string"}},
            "required": ["file_key"],
        },
        "annotations": {"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": True},
    },
    {
        "name": "creative_figma_comments",
        "description": "List comments on a Figma file.",
        "inputSchema": {
            "type": "object",
            "properties": {"file_key": {"type": "string"}},
            "required": ["file_key"],
        },
        "annotations": {"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": True},
    },
    {
        "name": "creative_canva_autofill",
        "description": "Autofill a Canva brand template with text/image fields.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "brand_template_id": {"type": "string"},
                "data": {"type": "object"},
                "title": {"type": "string"},
            },
            "required": ["brand_template_id", "data"],
        },
        "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True},
    },
    {
        "name": "creative_canva_upload_asset",
        "description": "Upload a local image asset to Canva.",
        "inputSchema": {
            "type": "object",
            "properties": {"file_path": {"type": "string"}, "name": {"type": "string"}},
            "required": ["file_path"],
        },
        "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True},
    },
    {
        "name": "creative_image_generate",
        "description": "Generate an image with OpenAI Images API.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "prompt": {"type": "string"},
                "output_path": {"type": "string"},
                "size": {"type": "string"},
                "quality": {"type": "string"},
            },
            "required": ["prompt", "output_path"],
        },
        "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True},
    },
    {
        "name": "creative_image_edit",
        "description": "Edit an existing image with OpenAI Images API.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "image_path": {"type": "string"},
                "prompt": {"type": "string"},
                "output_path": {"type": "string"},
            },
            "required": ["image_path", "prompt", "output_path"],
        },
        "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True},
    },
    {
        "name": "creative_video_submit",
        "description": "Submit a video generation job to Kling or Runway.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "provider": {"type": "string", "enum": ["kling", "runway"]},
                "prompt": {"type": "string"},
                "duration": {"type": "integer"},
            },
            "required": ["provider", "prompt"],
        },
        "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": True},
    },
    {
        "name": "creative_video_poll",
        "description": "Poll a Kling or Runway video generation job status.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "provider": {"type": "string", "enum": ["kling", "runway"]},
                "task_id": {"type": "string"},
            },
            "required": ["provider", "task_id"],
        },
        "annotations": {"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": True},
    },
]


def call_tool(name: str, args: Dict[str, Any]) -> Dict[str, Any]:
    if name == "creative_figma_summary":
        return figma_client.get_file_summary(args["file_key"])
    if name == "creative_figma_comments":
        return figma_client.list_comments(args["file_key"])
    if name == "creative_canva_autofill":
        return canva_client.autofill_design(
            args["brand_template_id"], args.get("data") or {}, title=args.get("title")
        )
    if name == "creative_canva_upload_asset":
        return canva_client.upload_asset(args["file_path"], name=args.get("name"))
    if name == "creative_image_generate":
        return openai_image_client.generate_image(
            args["prompt"],
            args["output_path"],
            size=args.get("size", "1024x1024"),
            quality=args.get("quality", "medium"),
        )
    if name == "creative_image_edit":
        return openai_image_client.edit_image(
            args["image_path"], args["prompt"], args["output_path"]
        )
    if name == "creative_video_submit":
        mod = kling_video_client if args["provider"] == "kling" else runway_video_client
        return mod.submit_video(args["prompt"], duration=args.get("duration", 5))
    if name == "creative_video_poll":
        mod = kling_video_client if args["provider"] == "kling" else runway_video_client
        return mod.poll_video(args["task_id"])
    raise ValueError(f"Unknown tool: {name}")


def dispatch(msg: Dict[str, Any]) -> Dict[str, Any] | None:
    method = msg.get("method")
    if "id" not in msg:
        return None
    reply: Dict[str, Any] = {"jsonrpc": "2.0", "id": msg["id"]}
    if method == "initialize":
        return {
            **reply,
            "result": {
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "aria-creative-media", "version": "0.1.0"},
            },
        }
    if method == "ping":
        return {**reply, "result": {}}
    if method == "tools/list":
        return {**reply, "result": {"tools": TOOLS}}
    if method == "tools/call":
        params = msg.get("params") or {}
        try:
            res = call_tool(str(params.get("name") or ""), dict(params.get("arguments") or {}))
            return {
                **reply,
                "result": {
                    "content": [{"type": "text", "text": json.dumps(res, ensure_ascii=False)}],
                    "isError": False,
                },
            }
        except Exception as exc:
            return {
                **reply,
                "result": {"content": [{"type": "text", "text": str(exc)}], "isError": True},
            }
    return {**reply, "error": {"code": -32601, "message": "Method not found"}}


def main() -> None:
    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        try:
            resp = dispatch(json.loads(line))
        except Exception:
            resp = {"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "Parse error"}}
        if resp is not None:
            print(json.dumps(resp, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
