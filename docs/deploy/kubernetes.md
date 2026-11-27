# Kubernetes

plsremind needs nothing particular from a cluster: two Deployments, a database and an Ingress.
This guide is not tested in CI; the [Compose file](compose.md) is the tested form of the same
setup. Adapt it to how your cluster does things, for example with an operator for PostgreSQL or
a database you already run.

Two rules come from plsremind itself:

- **One hub, replaced with `Recreate`.** The hub updates the database when it starts, and two
  hubs must never do that at the same time.
- **One bot per Discord application, replaced with `Recreate`**, with a volume of its own. A
  second copy would send every reminder twice.

## The secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: plsremind
stringData:
  PLSREMIND_SECRET_KEY: "<openssl rand -base64 32>"
  POSTGRES_PASSWORD: "<a password>"
  DISCORD_TOKEN: "<the bot token>"
  PLSREMIND_DISCORD_TOKEN: "<plsr_con_...>"
```

## PostgreSQL

With a PostgreSQL you already run, give the hub its address in `PLSREMIND_DATABASE_URL` and
leave this out. Otherwise, one in the cluster:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
spec:
  serviceName: db
  replicas: 1
  selector:
    matchLabels: {app: plsremind-db}
  template:
    metadata:
      labels: {app: plsremind-db}
    spec:
      containers:
        - name: postgres
          image: postgres:18
          env:
            - {name: POSTGRES_USER, value: plsremind}
            - {name: POSTGRES_DB, value: plsremind}
            - name: POSTGRES_PASSWORD
              valueFrom: {secretKeyRef: {name: plsremind, key: POSTGRES_PASSWORD}}
          ports:
            - containerPort: 5432
          volumeMounts:
            - {name: data, mountPath: /var/lib/postgresql}
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests: {storage: 5Gi}
---
apiVersion: v1
kind: Service
metadata:
  name: db
spec:
  selector: {app: plsremind-db}
  ports:
    - port: 5432
```

## The hub

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hub
spec:
  replicas: 1
  strategy: {type: Recreate}
  selector:
    matchLabels: {app: plsremind-hub}
  template:
    metadata:
      labels: {app: plsremind-hub}
    spec:
      containers:
        - name: hub
          image: ghcr.io/rayakame/plsremind:0.0
          # The series tag moves with every fix; a restart then pulls the newest.
          imagePullPolicy: Always
          env:
            - {name: PLSREMIND_DATABASE_URL, value: "postgres://plsremind@db:5432/plsremind?sslmode=disable"}
            - name: PGPASSWORD
              valueFrom: {secretKeyRef: {name: plsremind, key: POSTGRES_PASSWORD}}
            - name: PLSREMIND_SECRET_KEY
              valueFrom: {secretKeyRef: {name: plsremind, key: PLSREMIND_SECRET_KEY}}
            - {name: PLSREMIND_PUBLIC_URL, value: "https://todo.example.com"}
            - {name: PLSREMIND_PROXY_HEADER, value: X-Forwarded-For}
            - {name: PLSREMIND_METRICS_ADDR, value: ":9090"}
          ports:
            - {name: http, containerPort: 8080}
            - {name: metrics, containerPort: 9090}
          # Migrations run before the hub listens; a long one gets five minutes.
          startupProbe:
            httpGet: {path: /readyz, port: http}
            periodSeconds: 10
            failureThreshold: 30
          readinessProbe:
            httpGet: {path: /readyz, port: http}
            periodSeconds: 10
          livenessProbe:
            httpGet: {path: /healthz, port: http}
            periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: hub
spec:
  selector: {app: plsremind-hub}
  ports:
    - {name: http, port: 8080, targetPort: http}
```

The hub ends while the database is not ready yet, and Kubernetes starts it again.

## The Ingress

Until the first account exists, whoever reaches the hub first becomes its admin. Create it
before the Ingress: with `kubectl port-forward service/hub 8080:8080` running, make the call in
[docs/self-hosting.md](../self-hosting.md#first-steps) against `http://localhost:8080`.

The gateway is a WebSocket that stays open; the Ingress must not cut it after less than a
minute (see [reverse proxies and tunnels](proxies.md#websockets)). With ingress-nginx, and the
certificate in the secret `plsremind-tls`, for example from cert-manager:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: plsremind
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "120"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "120"
spec:
  ingressClassName: nginx
  tls:
    - hosts: [todo.example.com]
      secretName: plsremind-tls
  rules:
    - host: todo.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service: {name: hub, port: {name: http}}
```

## The Discord bot

After the Discord side is set up as in [docs/self-hosting.md](../self-hosting.md#the-discord-bot):

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: discord
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests: {storage: 1Gi}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: discord
spec:
  replicas: 1
  strategy: {type: Recreate}
  selector:
    matchLabels: {app: plsremind-discord}
  template:
    metadata:
      labels: {app: plsremind-discord}
    spec:
      # The image runs as user 10001, which must be able to write to the volume.
      securityContext: {fsGroup: 10001}
      terminationGracePeriodSeconds: 20
      containers:
        - name: discord
          image: ghcr.io/rayakame/plsremind-discord:0.0
          # The series tag moves with every fix; a restart then pulls the newest.
          imagePullPolicy: Always
          env:
            - name: DISCORD_TOKEN
              valueFrom: {secretKeyRef: {name: plsremind, key: DISCORD_TOKEN}}
            - name: PLSREMIND_TOKEN
              valueFrom: {secretKeyRef: {name: plsremind, key: PLSREMIND_DISCORD_TOKEN}}
            - {name: PLSREMIND_URL, value: "http://hub:8080"}
            - {name: PLSREMIND_PUBLIC_URL, value: "https://todo.example.com"}
          volumeMounts:
            - {name: data, mountPath: /data}
      volumes:
        - name: data
          persistentVolumeClaim: {claimName: discord}
```

## Updating and backups

Within a series, restart the Deployments; with `imagePullPolicy: Always` they pull the newest
image of the series: `kubectl rollout restart deployment/hub deployment/discord`. For a new
series, see [updating](updating.md).

The commands of [backups](backups.md) work with `kubectl exec -i db-0 --` in place of
`docker compose exec -T db`. In place of stopping the Compose services, scale the hub (and the
bot, if you run it) to 0 and wait until its pod is gone, then back to 1 afterwards:

```sh
kubectl scale deployment/hub --replicas=0
kubectl wait --for=delete pod -l app=plsremind-hub --timeout=120s
```
