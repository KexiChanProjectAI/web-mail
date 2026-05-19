# Backup and Disaster Recovery

This guide covers backing up lite-mail data, which consists of:
- MariaDB database (email metadata, recipient info, attachment metadata)
- Filesystem storage (raw MIME files and attachment content)

## What Needs Backing Up

### Database Contents

- `messages` table: email metadata, sender, subject, date, body text, raw MIME path reference
- `message_recipients` table: normalized recipient list
- `attachments` table: attachment metadata (storage key, filename, mime type, size, hash)

### Filesystem Storage (DATA_DIR)

```
DATA_DIR/
├── raw/           # Raw MIME email files
└── attachments/   # Attachment content files
```

The Go service stores raw MIME in `DATA_DIR/raw/` and attachments in `DATA_DIR/attachments/`. The database contains references to these files via `raw_mime_path` and `storage_key` columns.

## Filesystem Backup

### What's Stored

- Raw MIME files in `DATA_DIR/raw/`
- Attachment files in `DATA_DIR/attachments/`

### Backup Strategy

#### rsync to Backup Location

```bash
# Create a timestamped backup
rsync -avz --delete /var/lib/lite-mail/data/ /backup/lite-mail-data-$(date +%Y%m%d)/
```

#### Incremental Backup with rsync

```bash
# First backup
rsync -avz /var/lib/lite-mail/data/ /backup/lite-mail/latest/

# Subsequent backups (only changes are copied)
rsync -avz --delete /var/lib/lite-mail/data/ /backup/lite-mail/latest/
```

#### Filesystem Snapshots

If using LVM or ZFS:

```bash
# LVM snapshot backup
lvcreate -L10G -s -n lite-mail-snap /dev/vg00/lite-mail
mount /dev/vg00/lite-mail-snap /mnt/snap
rsync -avz /mnt/snap/ /backup/lite-mail/
umount /mnt/snap
lvdelete /dev/vg00/lite-mail-snap
```

### Restore Procedure

```bash
# Stop the service
systemctl stop lite-mail

# Restore files
rsync -avz /backup/lite-mail-data-20240115/ /var/lib/lite-mail/data/

# Fix permissions
chown -R lite-mail:lite-mail /var/lib/lite-mail/data/

# Start the service
systemctl start lite-mail
```

## Database Backup

### mysqldump

```bash
# Single database dump
mysqldump -u lite_mail -p lite_mail > backup-lite-mail-$(date +%Y%m%d).sql

# Compressed dump
mysqldump -u lite_mail -p lite_mail | gzip > backup-lite-mail-$(date +%Y%m%d).sql.gz
```

### mariabackup (Hot Backup)

For zero-downtime backups on running MariaDB:

```bash
# Full backup
mariabackup --backup --target-dir /backup/mariabackup-$(date +%Y%m%d) --user lite_mail --password

# Prepare and restore
mariabackup --prepare --target-dir /backup/mariabackup-20240115
mariabackup --copy-back --target-dir /backup/mariabackup-20240115
```

### Restore Procedure

```bash
# Stop the service
systemctl stop lite-mail

# Restore database
mysql -u lite_mail -p lite_mail < backup-lite-mail-20240115.sql

# Or for compressed dumps
gunzip < backup-lite-mail-20240115.sql.gz | mysql -u lite_mail -p lite_mail

# Start the service
systemctl start lite-mail
```

## Backup Schedule

| Backup Type | Frequency | Retention |
|-------------|-----------|-----------|
| Database (mysqldump) | Daily | 7 days |
| Database (mysqldump) | Weekly | 4 weeks |
| Filesystem (rsync) | Daily | 7 days |
| Filesystem (rsync) | Weekly | 4 weeks |
| Full snapshot | Monthly | 12 months |

Coordinate database and filesystem backups to run at the same time to ensure consistency between the two.

## Automated Backup Script

Example script for `/etc/cron.daily/backup-lite-mail`:

```bash
#!/bin/bash
set -e

BACKUP_DIR=/backup/lite-mail
DATE=$(date +%Y%m%d)
DATA_DIR=/var/lib/lite-mail/data

# Ensure backup directory exists
mkdir -p $BACKUP_DIR

# Backup database
mysqldump -u lite_mail -p'YOUR_PASSWORD' lite_mail | gzip > $BACKUP_DIR/database-$DATE.sql.gz

# Backup filesystem
rsync -avz --delete $DATA_DIR $BACKUP_DIR/filesystem-$DATE/

# Remove old backups (keep last 7 days)
find $BACKUP_DIR -type f -mtime +7 -delete
find $BACKUP_DIR -type d -mtime +7 -exec rm -rf {} \;

echo "Backup completed: $DATE"
```

## Disaster Recovery Procedure

### Scenario: Complete Server Loss

1. **Provision new server** with same OS version

2. **Install dependencies**
   ```bash
   apt install mariadb-server golang
   ```

3. **Install Go binary**
   ```bash
   go build -o /usr/local/bin/lite-mail ./cmd/server
   ```

4. **Restore MariaDB**
   ```bash
   mysql -u root -p -e "CREATE DATABASE lite_mail CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
   mysql -u lite_mail -p lite_mail < /backup/lite-mail-database-20240115.sql
   ```

5. **Restore DATA_DIR**
   ```bash
   rsync -avz /backup/lite-mail-filesystem-20240115/data/ /var/lib/lite-mail/data/
   chown -R lite-mail:lite-mail /var/lib/lite-mail/data/
   ```

6. **Configure and start service**
   ```bash
   cp /path/to/.env /opt/lite-mail/.env
   systemctl enable --now lite-mail
   ```

7. **Reconfigure Cloudflare DNS** if the new server has a different IP address

### Verification After Restore

```bash
# Check service is running
systemctl status lite-mail

# Check health endpoint
curl https://mail.example.com/healthz

# Verify data integrity
mysql -u lite_mail -p lite_mail -e "SELECT COUNT(*) FROM messages;"
ls -la /var/lib/lite-mail/data/raw/ | head -20
```

## Backup Verification

Periodically test restore procedures on a staging server to ensure backups are valid:

1. Provision a test server with the same setup
2. Restore latest database backup
3. Restore latest filesystem backup
4. Start the service
5. Verify emails and attachments are accessible
6. Check for any corruption or missing data
