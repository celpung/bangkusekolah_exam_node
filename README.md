# Bangku Sekolah Exam Node

Exam Node is the isolated runtime for delegated student exam sessions. It receives exam bundles from Central, serves the student sitting flow, stores attempts locally, and harvests completed attempts back to Central.

## Production installation

### Requirements

Before installing a node:

1. Register the Exam Node in Central as a `SuperAdmin` or `Admin`.
2. Save the Central node token. It is a secret and must not be written to logs or documentation.
3. Publish the exam and deploy it to this Exam Node before the normal installation runs its first bundle pull.
4. Point a DNS `A` record to the VPS public IP:

   ```text
   exam-node1.example.com -> VPS_PUBLIC_IP
   ```

5. During the first certificate issuance, use DNS-only mode and remove any `AAAA` record that points to an unavailable IPv6 address.
6. Allow TCP ports `80` and `443` in both the VPS firewall and the provider firewall.

The installer supports Debian and Ubuntu hosts with `apt-get`. The VPS needs Docker, not Go.

### Open the firewall

Run as `root` on the VPS:

```bash
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw reload
ufw status verbose
```

### Run the one-command installer

```bash
curl -fsSL https://services.bangkusekolah.com/node/setup.sh | sh
```

When prompted, enter:

```text
Central base URL:
https://services.bangkusekolah.com
```

```text
Central node token:
[PASTE THE TOKEN FROM CENTRAL; INPUT IS HIDDEN]
```

```text
Exam Node domain/URL for Nginx:
exam-node1.example.com
```

```text
ACME/Let's Encrypt email:
ops@example.com
```

The bootstrap installs Docker and selects the available Docker Compose package (`docker-compose-v2` or `docker-compose-plugin`). It then clones the Exam Node repository from the `main` branch into:

```text
/opt/bangkusekolah/exam-node
```

The setup process installs and configures Nginx and Certbot, obtains the TLS certificate, builds the operational tools, starts MySQL and Exam Node, pulls the deployed bundles, runs preflight, and verifies readiness.

Do not use the Central service binary as the node runtime. The Exam Node image is built from:

```text
./cmd/examnode
```

## Verify the installation

```bash
cd /opt/bangkusekolah/exam-node
docker compose ps
```

Both services should be healthy:

```text
examnode   healthy
mysql      healthy
```

Check the local probes:

```bash
curl -fsS http://127.0.0.1:8080/livez
curl -fsS http://127.0.0.1:8080/readyz
```

Check the public HTTPS endpoint:

```bash
curl -fsS https://exam-node1.example.com/livez
curl -fsS https://exam-node1.example.com/readyz
```

Run the operational preflight:

```bash
cd /opt/bangkusekolah/exam-node
sudo scripts/maintenance/run-tool.sh preflight
```

The expected result is:

```text
PASS
```

Do not allow students to use the node when preflight reports `FAIL`.

## Pull a new exam

In Central:

```text
Create the exam
-> add the questions
-> publish the exam
-> deploy the exam to this Exam Node
```

On the VPS:

```bash
cd /opt/bangkusekolah/exam-node
sudo scripts/maintenance/run-tool.sh bundleload --pull
sudo scripts/maintenance/run-tool.sh preflight
```

`bundleload --pull` pulls all active deployments assigned to this node, verifies their checksums, and refreshes the running cache.

Use the wrapper above. The operational tools are not inside the long-running `examnode` container, so this is incorrect:

```bash
docker exec -it exam-node-examnode-1 bundleload --pull
```

## Exam window and attempt duration

The exam window controls when a student may start:

```text
starts_at <= current server time < ends_at
```

The student attempt duration is controlled separately by `duration_minutes`. For example:

```text
Exam window:       1 Sep 22:00 -> 2 Sep 22:00
Attempt duration:  60 minutes
```

A student who starts at 22:00 receives a 60-minute attempt, not a 24-hour attempt. The server remains authoritative for the exam window, attempt deadline, autosave, and submit operations.

## Monitor the node

```bash
cd /opt/bangkusekolah/exam-node
docker compose logs --tail=100 examnode
```

Follow Exam Node logs:

```bash
docker compose logs -f examnode
```

Force a harvest when required:

```bash
sudo scripts/maintenance/run-tool.sh examharvest --force
```

## TLS troubleshooting

If Certbot returns `522`, a timeout, or an unauthorized challenge error:

1. Confirm the DNS `A` record points to the VPS public IP.
2. Confirm the record is DNS-only during initial certificate issuance.
3. Confirm ports `80` and `443` are allowed by UFW and the provider firewall.
4. Confirm Nginx is listening on port `80`:

   ```bash
   nginx -t
   ss -ltnp | grep ':80'
   ```

5. Test the ACME webroot from an external network:

   ```bash
   mkdir -p /var/www/certbot/.well-known/acme-challenge
   printf 'challenge-ok' > /var/www/certbot/.well-known/acme-challenge/check
   ```

   ```bash
   curl -fsS http://exam-node1.example.com/.well-known/acme-challenge/check
   ```

   The response should be:

   ```text
   challenge-ok
   ```

Do not repeatedly retry Certbot until the challenge file is reachable from the public internet.

## Install infrastructure without an exam

The normal one-command flow expects an exam deployment so that bundle loading and preflight can complete. For infrastructure preparation from an existing checkout, use:

```bash
cd /opt/bangkusekolah/exam-node
sudo ./setup.sh -skip_pull -skip_preflight
```

After an exam has been published and deployed:

```bash
sudo scripts/maintenance/run-tool.sh bundleload --pull
sudo scripts/maintenance/run-tool.sh preflight
```

Skipping preflight is not a production readiness result.

## Important security notes

- Never write Central node tokens, JWT secrets, database passwords, access codes, or DSNs to this README, shell history, logs, or support reports.
- Keep Central JWT credentials separate from Exam Node student JWT credentials.
- Do not expose MySQL on a public interface. The Compose configuration binds MySQL to loopback on the VPS.
- Do not destroy the node while unharvested attempts remain.
- Do not modify the exam bundle or its database manually while students are sitting the exam.
