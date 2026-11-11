"""Exercise issue #9 before and after an actual server/container restart.

Only the standard library is required. The state file contains a synthetic demo
session; keep it under ignored tmp/, and remove it after verification.
"""

import argparse
import json
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from http.cookies import SimpleCookie


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("phase", choices=["prepare", "verify"])
    parser.add_argument("--base", default="http://localhost:8080")
    parser.add_argument("--state", default="tmp/session-plan-smoke.json")
    args = parser.parse_args()
    state_path = Path(args.state)
    cookie = ""

    def call(method, path, body=None, expected=200):
        headers = {"Origin": args.base, "Accept": "application/json"}
        if cookie:
            headers["Cookie"] = cookie
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        request = Request(args.base + "/api/v1" + path, data=data, headers=headers, method=method)
        try:
            response = urlopen(request, timeout=10)
        except HTTPError as error:
            response = error
        with response:
            content = response.read()
            assert response.status == expected, (method, path, response.status, content)
            assert response.headers.get("Cache-Control") == "no-store"
            result = json.loads(content) if content else None
            return result, response.headers

    if args.phase == "prepare":
        accounts, _ = call("GET", "/demo/accounts")
        roles = {account["id"]: account["role"] for account in accounts}
        assert roles.get("intern_demo_1") == "intern", roles
        assert roles.get("mentor_demo_1") == "mentor", roles
        assert roles.get("mentor_demo_2") == "mentor", roles
        user, headers = call("POST", "/demo/session", {"account_id": "intern_demo_1"})
        assert user["id"] == "intern_demo_1"
        parsed_cookie = SimpleCookie()
        parsed_cookie.load(headers["Set-Cookie"])
        value = parsed_cookie["buddy_session"]
        assert value["httponly"] and value["samesite"] == "Lax" and value["path"] == "/"
        cookie = "buddy_session=" + value.value
        call("GET", "/me")
        plan, _ = call("GET", "/onboarding/plan")
        assert plan["id"] == "plan_demo_1"
        call("PATCH", "/onboarding/steps/step_local_setup", {"status": "completed"}, expected=409)
        plan, _ = call("PATCH", "/onboarding/steps/step_local_setup", {"status": "not_started"})
        assert plan["steps"][0]["status"] == "not_started"
        state_path.parent.mkdir(parents=True, exist_ok=True)
        state_path.write_text(json.dumps({"cookie": cookie}), encoding="utf-8")
        print("PASS: demo login, me, plan, forbidden completion and saved progress; restart server now")
    else:
        cookie = json.loads(state_path.read_text(encoding="utf-8"))["cookie"]
        user, _ = call("GET", "/me")
        assert user["id"] == "intern_demo_1"
        plan, _ = call("GET", "/onboarding/plan")
        assert plan["steps"][0]["status"] == "not_started"
        call("PATCH", "/onboarding/steps/step_local_setup", {"status": "in_progress"})
        call("DELETE", "/session", expected=204)
        call("DELETE", "/session", expected=204)
        call("GET", "/me", expected=401)
        state_path.unlink()
        print("PASS: session and progress survived restart, logout revoked the session")


if __name__ == "__main__":
    main()
