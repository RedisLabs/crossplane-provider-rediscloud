"""CI-only experiments; these do not qualify real Cloud serialization."""
import multiprocessing
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest

import qa_account_simulator as sim


def competitor(path, start, result, owner):
    start.wait(10)
    result.put(sim.acquire(path, owner, 1))


def exit_after_admission(path):
    os._exit(0 if sim.admit(path, 'run', 1, 1, 'crashed') else 1)


def uncommitted_writer(path, ready, release):
    db = sqlite3.connect(path, isolation_level=None)
    db.execute('BEGIN IMMEDIATE')
    db.execute("INSERT INTO intents VALUES ('uncommitted', 'run', 1, 'ADMITTED')")
    ready.set()
    release.wait(30)
    db.rollback()
    db.close()


class PausingConnection:
    """Pause a real simulator transaction while it holds its SQLite write lock."""
    def __init__(self, db, pause_prefix, paused, resume):
        self.db, self.pause_prefix, self.paused, self.resume = db, pause_prefix, paused, resume

    def execute(self, sql, *args):
        result = self.db.execute(sql, *args)
        if sql.startswith(self.pause_prefix):
            self.paused.set()
            if not self.resume.wait(20):
                raise TimeoutError('test did not resume the transaction')
        return result

    def __getattr__(self, name):
        return getattr(self.db, name)


def paused_operation(path, kind, paused, resume, results):
    original = sim.connect
    prefix = 'SELECT owner,attempt' if kind == 'admit' else "UPDATE account SET phase='DRAINING'"
    sim.connect = lambda name: PausingConnection(original(name), prefix, paused, resume)
    result = sim.admit(path, 'run', 1, 1, 'racing') if kind == 'admit' else sim.drain(path, 'run', 1, 1)
    results.put((kind, result))


class AttemptingConnection:
    def __init__(self, db, starting):
        self.db, self.starting = db, starting

    def execute(self, sql, *args):
        if sql == 'BEGIN IMMEDIATE':
            self.starting.set()
        return self.db.execute(sql, *args)

    def __getattr__(self, name):
        return getattr(self.db, name)


def competing_operation(path, kind, starting, finished, results):
    original = sim.connect
    sim.connect = lambda name: AttemptingConnection(original(name), starting)
    try:
        result = sim.admit(path, 'run', 1, 1, 'racing') if kind == 'admit' else sim.drain(path, 'run', 1, 1)
        results.put((kind, result))
    finally:
        finished.set()


class CoordinatorExperiment(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.db = str(Path(self.tmp.name) / 'state.db')
        sim.initialize(self.db)

    def cleanup_process(self, job):
        if job.is_alive():
            job.terminate()
            job.join(20)

    def test_independent_processes_have_one_winner(self):
        ctx = multiprocessing.get_context('spawn')
        ready = ctx.Event()
        results = ctx.Queue()
        jobs = [ctx.Process(target=competitor, args=(self.db, ready, results, f'run-{n}')) for n in range(2)]
        for job in jobs:
            job.start()
            self.addCleanup(self.cleanup_process, job)
        ready.set()
        values = [results.get(timeout=20) for _ in jobs]
        for job in jobs:
            job.join(20)
            self.assertEqual(job.exitcode, 0)
        self.assertEqual(sorted(values), [False, True])

    def test_restart_and_run_attempt_do_not_clear_owner(self):
        self.assertTrue(sim.acquire(self.db, 'run', 1))
        self.assertFalse(sim.acquire(self.db, 'run', 2))
        self.assertFalse(sim.admit(self.db, 'run', 2, 1, 'req'))
        sim.initialize(self.db)
        self.assertFalse(sim.acquire(self.db, 'other', 1))
        self.assertTrue(sim.admit(self.db, 'run', 1, 1, 'req'))
        self.assertFalse(sim.admit(self.db, 'run', 1, 1, 'req'))

    def test_drain_and_unknown_fail_closed(self):
        self.assertTrue(sim.acquire(self.db, 'run', 1))
        self.assertFalse(sim.admit(self.db, 'run', 1, 2, 'stale'))
        self.assertTrue(sim.admit(self.db, 'run', 1, 1, 'first'))
        self.assertTrue(sim.admit(self.db, 'run', 1, 1, 'second'))
        self.assertTrue(sim.drain(self.db, 'run', 1, 1))
        self.assertFalse(sim.admit(self.db, 'run', 1, 1, 'late'))
        self.assertTrue(sim.unknown(self.db, 'run', 1, 1, 'first'))
        self.assertTrue(sim.unknown(self.db, 'run', 1, 1, 'second'))
        self.assertFalse(sim.acquire(self.db, 'other', 1))
        with sqlite3.connect(self.db) as db:
            self.assertEqual(db.execute('SELECT phase FROM account').fetchone(), ('HOLD',))
            self.assertEqual(db.execute("SELECT count(*) FROM intents WHERE outcome='UNKNOWN'").fetchone(), (2,))

    def test_committed_admission_visible_after_writer_exit(self):
        self.assertTrue(sim.acquire(self.db, 'run', 1))
        ctx = multiprocessing.get_context('spawn')
        job = ctx.Process(target=exit_after_admission, args=(self.db,))
        job.start()
        self.addCleanup(self.cleanup_process, job)
        job.join(20)
        self.assertEqual(job.exitcode, 0)
        self.assertFalse(sim.acquire(self.db, 'other', 1))
        with sqlite3.connect(self.db) as db:
            self.assertEqual(db.execute("SELECT outcome FROM intents WHERE request_id='crashed'").fetchone(), ('ADMITTED',))

    def test_killed_uncommitted_writer_rolls_back_without_releasing_owner(self):
        self.assertTrue(sim.acquire(self.db, 'run', 1))
        ctx = multiprocessing.get_context('spawn')
        ready, release = ctx.Event(), ctx.Event()
        job = ctx.Process(target=uncommitted_writer, args=(self.db, ready, release))
        job.start()
        self.addCleanup(self.cleanup_process, job)
        self.assertTrue(ready.wait(20))
        job.terminate()
        job.join(20)
        self.assertFalse(job.is_alive())
        self.assertFalse(sim.acquire(self.db, 'other', 1))
        with sqlite3.connect(self.db) as db:
            self.assertEqual(db.execute("SELECT count(*) FROM intents WHERE request_id='uncommitted'").fetchone(), (0,))

    def test_drain_and_admission_interleavings(self):
        # Exercise both orders using actual SQLite transactions and processes.
        # No external Cloud or production store is involved.
        for first in ('admit', 'drain'):
            with self.subTest(first=first), tempfile.TemporaryDirectory() as temp:
                path = str(Path(temp) / 'state.db')
                sim.initialize(path)
                self.assertTrue(sim.acquire(path, 'run', 1))
                ctx = multiprocessing.get_context('spawn')
                paused, resume, starting, finished = ctx.Event(), ctx.Event(), ctx.Event(), ctx.Event()
                results = ctx.Queue()
                first_job = ctx.Process(target=paused_operation, args=(path, first, paused, resume, results))
                first_job.start()
                self.addCleanup(self.cleanup_process, first_job)
                self.assertTrue(paused.wait(20))
                second = 'drain' if first == 'admit' else 'admit'
                second_job = ctx.Process(target=competing_operation, args=(path, second, starting, finished, results))
                second_job.start()
                self.addCleanup(self.cleanup_process, second_job)
                self.assertTrue(starting.wait(20))
                self.assertFalse(finished.wait(0.5), "contender completed while the first write transaction was paused")
                resume.set()
                outcomes = dict(results.get(timeout=20) for _ in range(2))
                for job in (first_job, second_job):
                    job.join(20)
                    self.assertEqual(job.exitcode, 0)
                self.assertTrue(outcomes['drain'])
                self.assertEqual(outcomes['admit'], first == 'admit')
                self.assertFalse(sim.admit(path, 'run', 1, 1, 'after-drain'))
                with sqlite3.connect(path) as db:
                    self.assertEqual(db.execute("SELECT count(*) FROM intents WHERE request_id='racing'").fetchone()[0], int(first == 'admit'))

    def test_wrong_identity_cannot_drain_or_mark_unknown(self):
        self.assertTrue(sim.acquire(self.db, 'run', 1))
        self.assertTrue(sim.admit(self.db, 'run', 1, 1, 'first'))
        self.assertFalse(sim.drain(self.db, 'other', 1, 1))
        self.assertFalse(sim.unknown(self.db, 'other', 1, 1, 'first'))
        self.assertTrue(sim.admit(self.db, 'run', 1, 1, 'second'))


if __name__ == '__main__':
    unittest.main()
