"""Loopback HTTPS fixture that sends part of a response before waiting for C-c."""

import socketserver
import ssl
import sys


def request(stream):
    line = stream.readline()
    while stream.readline() not in (b"\r\n", b""):
        pass
    return line


class Download(socketserver.StreamRequestHandler):
    def handle(self):
        try:
            self.connection.settimeout(30)
            if request(self.rfile) != b"CONNECT github.com:443 HTTP/1.1\r\n":
                self.wfile.write(b"HTTP/1.1 403 Forbidden\r\n\r\n")
                return
            self.wfile.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            with tls.wrap_socket(self.connection, server_side=True) as client:
                with client.makefile("rb") as stream:
                    if not request(stream).startswith(b"GET /duckdb/duckdb/releases/download/"):
                        client.sendall(b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
                        return
                client.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 1048576\r\n\r\n")
                client.sendall(bytes(65536))
                client.recv(1)
        except OSError:
            # C-c closes the connection with only a partial body delivered.
            return


tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
tls.load_cert_chain(sys.argv[1], sys.argv[2])
with socketserver.ThreadingTCPServer(("127.0.0.1", 0), Download) as server:
    server.daemon_threads = True
    print(server.server_address[1], flush=True)
    server.serve_forever()
