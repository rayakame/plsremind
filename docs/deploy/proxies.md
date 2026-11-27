# Reverse proxies and tunnels

The hub speaks plain HTTP on port 8080. Put something in front of it that terminates HTTPS, and
set two things:

- `PLSREMIND_PUBLIC_URL` to the address people type into their browser, exactly, without a
  trailing slash.
- `PLSREMIND_PROXY_HEADER` to the header that carries the client's address. Without it every
  visitor appears to come from the proxy and they all share one rate limit, so logging in fails
  for everyone once a few people try at the same time. The hub believes the header only from
  loopback and private addresses, which is where a proxy on the same machine or network comes
  from. The proxy has to set the header to the address it sees, in place of what the client
  sent: Caddy and Traefik do so on their own, nginx with
  `proxy_set_header X-Forwarded-For $remote_addr`. A proxy that passes the client's header on
  lets the client choose the address its rate limit counts; one that adds to it, as nginx's
  `$proxy_add_x_forwarded_for` does, lets clients in a private network do so, since the hub
  trusts private addresses as proxies.

## WebSockets

The gateway is a WebSocket at `/api/v1/gateway`. The proxy has to pass the upgrade through and
must not cut idle connections after less than a minute; clients send a heartbeat every 45
seconds. Caddy and Traefik do both on their own. For nginx, set `proxy_http_version 1.1`,
forward the `Upgrade` and `Connection` headers and raise `proxy_read_timeout` to at least 75
seconds.

## A proxy on the same machine

Point the proxy at port 8080 and set `PLSREMIND_PROXY_HEADER=X-Forwarded-For`. With Caddy, for
example:

```
todo.example.com {
    reverse_proxy localhost:8080
}
```

Keep the hub published on that machine alone, as `compose.yaml` and the `docker run` guide do:
`"127.0.0.1:8080:8080"`, `-p 127.0.0.1:8080:8080`. On all addresses, anyone in your own network
could reach the hub past the proxy, from a private address, and set the header to whatever they
like; Docker's own forwarding of IPv6 connections can even make outside clients look private.
Swarm's host mode cannot publish on one address alone, so in Swarm keep the port closed to
others with a firewall in front of the node, since one on the node itself, ufw for instance,
does not hold back ports that Docker publishes; or use a proxy on the network instead.

## A proxy on the plsremind network

A proxy that runs as a container, Traefik or Caddy for instance, can join the `plsremind`
network and reach the hub by its name, `hub:8080`. The hub then needs no published port: delete
its `ports` in the deploy file. Set `PLSREMIND_PROXY_HEADER=X-Forwarded-For`.

In Swarm, publish the proxy's own ports in host mode, as `stack.yaml` does for the hub. With
Swarm's usual publishing, every client reaches the proxy from an address of Swarm's own, the
proxy passes that on, and all visitors share one rate limit.

## Cloudflare Tunnel

A tunnel reaches the hub from the inside, so nothing needs to be opened to the internet.
Run `cloudflared` on the `plsremind` network, with the tunnel's token from the Cloudflare
dashboard. With Compose, as a project of its own:

```yaml
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    command: tunnel --no-autoupdate run
    environment:
      TUNNEL_TOKEN: ${TUNNEL_TOKEN:?set TUNNEL_TOKEN}
    restart: unless-stopped

networks:
  default:
    name: plsremind
    external: true
```

In Swarm, the same service goes into a stack of its own with `deploy.restart_policy` instead of
`restart`, or into your copy of `stack.yaml`.

In the Cloudflare dashboard, give the tunnel a public hostname whose service is
`http://hub:8080`. Then set:

- `PLSREMIND_PUBLIC_URL` to that hostname, such as `https://todo.example.com`;
- `PLSREMIND_PROXY_HEADER=CF-Connecting-IP`, the header in which Cloudflare passes the client's
  address.

Delete the hub's `ports` in the deploy file. The tunnel is then the only way in, and nobody can
send the hub a `CF-Connecting-IP` of their choosing: with the port published, anyone in your
own network comes from a private address and would be believed.
