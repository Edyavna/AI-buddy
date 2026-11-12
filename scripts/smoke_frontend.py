"""Check the production frontend and the Go proxy on the Compose origin."""

import json
import re
from urllib.error import HTTPError
from urllib.request import urlopen


BASE = "http://localhost:5173"


def get(path):
    try:
        return urlopen(BASE + path, timeout=10)
    except HTTPError as error:
        return error


def main():
    with get("/") as response:
        assert response.status == 200 and response.headers.get_content_type() == "text/html"
        page = response.read().decode("utf-8")
    asset = re.search(r'<script[^>]+src="(/assets/[^"]+\.js)"', page)
    assert asset, "The SPA is not serving the production bundle"
    with get(asset.group(1)) as response:
        assert response.status == 200
        assert response.headers.get_content_type() in {"text/javascript", "application/javascript"}
        assert response.read(), "The JavaScript bundle is empty"
    with get("/mentor/interns") as response:
        assert response.status == 200 and response.read().decode("utf-8") == page
    with get("/assets/missing.js") as response:
        assert response.status == 404
    with get("/api/v1/me") as response:
        assert response.status == 401
        assert response.headers.get_content_type() == "application/json"
        assert json.load(response)["error"]["code"] == "UNAUTHENTICATED"
    print("PASS: production JS, deep link fallback, missing asset and proxied API error")


if __name__ == "__main__":
    main()
