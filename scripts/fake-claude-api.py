#!/usr/bin/env python3
"""A fake Claude API for the Q&A GUI test: streams a canned answer to every
POST /v1/messages and records each request body as a line of JSON; GET
/v1/models lists two models.

usage: fake-claude-api.py <port file> <request log>

The answer depends on the last question: "picture" gets an SVG block,
"slow" streams for a long time (to test Stop), anything else gets text
with a code block. The port it listens on is written to <port file>.
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT_FILE, LOG = sys.argv[1], sys.argv[2]

SVG = ('<svg xmlns="http://www.w3.org/2000/svg" width="240" height="120">'
       '<rect x="10" y="10" width="220" height="100" fill="#d00000"/>'
       '<text x="30" y="70" font-size="28" fill="#ffffff">fake svg</text></svg>')


def question(body):
    content = body["messages"][-1]["content"]
    return content if isinstance(content, str) else " ".join(b.get("text", "") for b in content)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def event(self, name, data):
        self.wfile.write(f"event: {name}\ndata: {json.dumps(data)}\n\n".encode())
        self.wfile.flush()

    def do_GET(self):
        models = [{"type": "model", "id": i, "display_name": n, "lifecycle": "active",
                   "created_at": "2026-01-01T00:00:00Z"}
                  for i, n in [("claude-test-model", "Test Model"), ("claude-other-model", "Other Model")]]
        data = json.dumps({"data": models, "has_more": False, "first_id": None, "last_id": None}).encode()
        self.send_response(200 if self.path.startswith("/v1/models") else 404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        with open(LOG, "a") as f:
            f.write(json.dumps({"beta": self.headers.get("anthropic-beta"), "body": body}) + "\n")
        q = question(body)
        if "picture" in q:
            pieces = ["Here is a picture:\n\n```svg\n", SVG[:60], SVG[60:], "\n```\n\nA red box."]
        elif "slow" in q:
            pieces = [f"word{i} " for i in range(300)]
        else:
            pieces = ["The answer ", "is 42.\n\n```sh\necho 42\n```\n"]
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        self.event("message_start", {"type": "message_start", "message": {
            "id": "msg_fake", "type": "message", "role": "assistant", "model": body["model"],
            "content": [], "stop_reason": None, "stop_sequence": None,
            "usage": {"input_tokens": 12, "output_tokens": 1,
                      "cache_read_input_tokens": 8 if len(body["messages"]) > 1 else 0,
                      "cache_creation_input_tokens": 0}}})
        self.event("content_block_start", {"type": "content_block_start", "index": 0,
                                           "content_block": {"type": "text", "text": ""}})
        try:
            for p in pieces:
                self.event("content_block_delta", {"type": "content_block_delta", "index": 0,
                                                   "delta": {"type": "text_delta", "text": p}})
                time.sleep(0.2 if "slow" in q else 0.05)
            self.event("content_block_stop", {"type": "content_block_stop", "index": 0})
            self.event("message_delta", {"type": "message_delta",
                                         "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                                         "usage": {"output_tokens": 20}})
            self.event("message_stop", {"type": "message_stop"})
        except (BrokenPipeError, ConnectionResetError):
            pass  # the app stopped the answer


srv = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(PORT_FILE, "w") as f:
    f.write(str(srv.server_address[1]))
srv.serve_forever()
