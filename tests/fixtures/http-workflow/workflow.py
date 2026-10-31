"""Small real HTTP workflow; run with this directory as the workspace."""
import argparse
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
import urllib.request


def response():
    port = int(Path("port").read_text())
    with urllib.request.urlopen(f"http://127.0.0.1:{port}/", timeout=0.5) as reply:
        return reply.read()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=["prepare", "serve", "ready", "check"])
    parser.add_argument("--exit-code", type=int, default=0)
    args = parser.parse_args()
    if args.operation == "prepare":
        Path("payload").write_bytes(b"runcradle-http-ok\n")
    elif args.operation == "serve":
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(Path("payload").read_bytes())

            def log_message(self, *_):
                pass

        with HTTPServer(("127.0.0.1", 0), Handler) as server:
            Path("port").write_text(str(server.server_port))
            server.serve_forever()
    else:
        try:
            content = response()
            if content != Path("payload").read_bytes():
                return 9
        except (OSError, ValueError):
            return 9
        if args.operation == "check":
            Path("checked").write_bytes(content)
            print("HTTP response verified", flush=True)
            return args.exit_code
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
