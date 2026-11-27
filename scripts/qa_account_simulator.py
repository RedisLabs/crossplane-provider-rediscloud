"""Credential-free coordinator experiment. Never use as a Cloud writer lock.

SQLite on one runner cannot fence other machines or Cloud API credentials.
There is intentionally no release, network transport, or Cloud integration.
"""
import sqlite3
from contextlib import closing


def connect(path):
    db = sqlite3.connect(path, timeout=15, isolation_level=None)
    db.execute("PRAGMA busy_timeout=15000")
    return db


def initialize(path):
    with closing(connect(path)) as db:
        db.execute("CREATE TABLE IF NOT EXISTS account (id INTEGER PRIMARY KEY CHECK (id = 1), owner TEXT NOT NULL, attempt INTEGER NOT NULL, generation INTEGER NOT NULL, phase TEXT NOT NULL)")
        db.execute("CREATE TABLE IF NOT EXISTS intents (request_id TEXT PRIMARY KEY, owner TEXT NOT NULL, generation INTEGER NOT NULL, outcome TEXT NOT NULL)")


def acquire(path, owner, attempt):
    if not owner or not isinstance(attempt, int) or isinstance(attempt, bool) or attempt < 1:
        raise ValueError("invalid identity")
    with closing(connect(path)) as db:
        db.execute("BEGIN IMMEDIATE")
        if db.execute("SELECT 1 FROM account WHERE id=1").fetchone():
            db.rollback()
            return False
        db.execute("INSERT INTO account VALUES (1, ?, ?, 1, 'ACTIVE')", (owner, attempt))
        db.commit()
        return True


def admit(path, owner, attempt, generation, request_id):
    if not request_id:
        raise ValueError("invalid request id")
    with closing(connect(path)) as db:
        db.execute("BEGIN IMMEDIATE")
        row = db.execute("SELECT owner,attempt,generation,phase FROM account WHERE id=1").fetchone()
        if row != (owner, attempt, generation, "ACTIVE"):
            db.rollback()
            return False
        try:
            db.execute("INSERT INTO intents VALUES (?, ?, ?, 'ADMITTED')", (request_id, owner, generation))
        except sqlite3.IntegrityError:
            db.rollback()
            return False
        db.commit()
        return True


def drain(path, owner, attempt, generation):
    with closing(connect(path)) as db:
        db.execute("BEGIN IMMEDIATE")
        changed = db.execute("UPDATE account SET phase='DRAINING' WHERE id=1 AND owner=? AND attempt=? AND generation=? AND phase='ACTIVE'", (owner, attempt, generation)).rowcount
        db.commit()
        return changed == 1


def unknown(path, owner, attempt, generation, request_id):
    with closing(connect(path)) as db:
        db.execute("BEGIN IMMEDIATE")
        row = db.execute("SELECT owner,attempt,generation,phase FROM account WHERE id=1").fetchone()
        if row not in ((owner, attempt, generation, 'ACTIVE'), (owner, attempt, generation, 'DRAINING'), (owner, attempt, generation, 'HOLD')):
            db.rollback()
            return False
        changed = db.execute("UPDATE intents SET outcome='UNKNOWN' WHERE request_id=? AND owner=? AND generation=? AND outcome='ADMITTED'", (request_id, owner, generation)).rowcount
        if changed:
            db.execute("UPDATE account SET phase='HOLD' WHERE id=1")
        db.commit()
        return changed == 1
