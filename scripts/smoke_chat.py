"""Issue #10 HTTP/SSE smoke, including actual process/container death.

Standard library only. Run prepare, restart API, verify, crash (--docker or
--process-id for a disposable Windows test server), restart API, recover.
Synthetic cookies are saved only under ignored tmp/ and removed on success.
"""

import argparse
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import signal
import subprocess
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen
import uuid


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("phase", choices=["prepare", "verify", "crash", "recover"])
    parser.add_argument("--base", default="http://localhost:8080")
    parser.add_argument("--state", default="tmp/chat-smoke.json")
    target = parser.add_mutually_exclusive_group()
    target.add_argument("--docker", action="store_true")
    target.add_argument("--process-id", type=int)
    args = parser.parse_args()
    path = Path(args.state)
    state = {} if args.phase == "prepare" else json.loads(path.read_text(encoding="utf-8"))
    cookie = state.get("cookie", "")

    def request(method, endpoint, body=None, stream=False):
        headers = {"Origin": args.base, "Accept": "text/event-stream" if stream else "application/json"}
        if cookie:
            headers["Cookie"] = cookie
        data = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body).encode()
        req = Request(args.base + "/api/v1" + endpoint, data=data, headers=headers, method=method)
        try:
            return urlopen(req, timeout=15)
        except HTTPError as error:
            return error

    def call(method, endpoint, body=None, expected=200):
        with request(method, endpoint, body) as response:
            content = response.read()
            assert response.status == expected, (method, endpoint, response.status, content)
            return json.loads(content) if content else None

    def frames(response):
        event = None
        data = None
        for raw in response:
            line = raw.decode("utf-8").rstrip("\r\n")
            if line.startswith("event: "):
                event = line[7:]
            elif line.startswith("data: "):
                data = json.loads(line[6:])
            elif not line and event is not None:
                yield event, data
                event = data = None

    def send(conversation_id, client_id, text):
        endpoint = "/conversations/" + conversation_id + "/messages"
        with request("POST", endpoint, {"client_message_id": client_id, "text": text}, stream=True) as response:
            assert response.status == 200
            assert response.headers["Content-Type"] == "text/event-stream"
            assert "no-store" in response.headers["Cache-Control"]
            assert response.headers["X-Accel-Buffering"] == "no"
            events = list(frames(response))
        assert events[0][0] == "message.started"
        assert events[-1][0] == "message.completed", events
        assert sum(kind in ("message.completed", "message.failed") for kind, _ in events) == 1
        terminal = events[-1][1]["message"]
        text = "".join(data["text"] for kind, data in events if kind == "answer.delta")
        assert terminal["text"] == text and text
        assert terminal["status"] == "completed"
        assert terminal["sources"] == [] and terminal["request_ids"] == []
        assert terminal["client_message_id"] is None and terminal["in_reply_to_message_id"]
        return terminal

    def save():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(state), encoding="utf-8")

    if args.phase == "prepare":
        with request("POST", "/demo/session", {"account_id": "intern_demo_1"}) as response:
            assert response.status == 200
            parsed = SimpleCookie()
            parsed.load(response.headers["Set-Cookie"])
            cookie = "buddy_session=" + parsed["buddy_session"].value
        state["cookie"] = cookie
        client_id = str(uuid.uuid4())
        conversation = call("POST", "/conversations", {"client_conversation_id": client_id, "step_id": "step_local_setup"}, expected=201)
        repeated = call("POST", "/conversations", {"client_conversation_id": client_id, "step_id": "step_local_setup"})
        assert repeated["id"] == conversation["id"]
        state["conversation_id"] = conversation["id"]
        state["client_message_id"] = str(uuid.uuid4())
        terminal = send(conversation["id"], state["client_message_id"], "Тестовый вопрос 🚀")
        endpoint = "/conversations/" + conversation["id"] + "/messages"
        history = call("GET", endpoint)
        assert len(history) == 2 and history[1] == terminal
        conflict = call("POST", endpoint, {"client_message_id": state["client_message_id"], "text": "Тестовый вопрос 🚀"}, expected=409)
        assert conflict["error"]["code"] == "MESSAGE_ALREADY_EXISTS"
        state["terminal"] = terminal
        save()
        print("PASS: chat create/retry, real SSE, terminal committed, history and message deduplication")
    elif args.phase == "verify":
        assert call("GET", "/me")["id"] == "intern_demo_1"
        history = call("GET", "/conversations/" + state["conversation_id"] + "/messages")
        assert len(history) == 2 and history[1] == state["terminal"]
        print("PASS: conversation, complete message and session survived API/container restart")
    elif args.phase == "crash":
        assert args.docker or (args.process_id and args.process_id > 0), "Specify an explicit disposable server to kill"
        state["crash_client_id"] = str(uuid.uuid4())
        endpoint = "/conversations/" + state["conversation_id"] + "/messages"
        with request("POST", endpoint, {"client_message_id": state["crash_client_id"], "text": "Проверка аварийного завершения"}, stream=True) as response:
            assert response.status == 200
            started = next(frames(response))
            assert started[0] == "message.started"
            state["crash_assistant_id"] = started[1]["message_id"]
            deadline = time.monotonic() + 10
            while True:
                history = call("GET", endpoint)
                message = next(m for m in history if m["id"] == state["crash_assistant_id"])
                assert message["status"] == "generating", "Demo agent finished before kill; increase DEMO_CHAT_DELAY"
                if message["text"]:
                    state["partial"] = message["text"]
                    break
                assert time.monotonic() < deadline, "No periodic partial save"
                time.sleep(0.1)
            save()
            if args.docker:
                subprocess.run(["docker", "compose", "kill", "-s", "SIGKILL", "api"], check=True, timeout=20)
            else:
                # On Windows SIGTERM uses TerminateProcess (no graceful cleanup).
                assert os.name == "nt", "Use --docker for Linux SIGKILL verification"
                os.kill(args.process_id, signal.SIGTERM)
        print("PASS: generating reply and partial text persisted before forced process death")
    else:
        endpoint = "/conversations/" + state["conversation_id"] + "/messages"
        history = call("GET", endpoint)
        assert history[1] == state["terminal"], "Recovery changed a completed reply"
        message = next(m for m in history if m["id"] == state["crash_assistant_id"])
        assert message["status"] == "interrupted"
        assert message["error"]["error"]["code"] == "GENERATION_INTERRUPTED"
        assert message["text"].startswith(state["partial"]) and message["text"]
        conflict = call("POST", endpoint, {"client_message_id": state["crash_client_id"], "text": "Проверка аварийного завершения"}, expected=409)
        assert conflict["error"]["code"] == "MESSAGE_ALREADY_EXISTS"
        send(state["conversation_id"], str(uuid.uuid4()), "Новый вопрос после рестарта")
        call("DELETE", "/session", expected=204)
        path.unlink()
        print("PASS: killed generation recovered as interrupted, partial retained, duplicate rejected, new generation works")


if __name__ == "__main__":
    main()
