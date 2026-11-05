"""Fixture resets for the 0.5.4 / Wails 3.1.0 re-acceptance batch.

The historical acceptance scripts are not idempotent and are left untouched here:
each completed run leaves behind state that the next run asserts against. This
helper restores those starting states, and it archives the previous session's
files instead of overwriting them, so earlier evidence survives.

  audit053: native-audit053-acceptance.mjs inserts one corrupt favourite during the
            run, so a re-run would start from two archives and fail its
            "one archive" assertion.
  desktop05: the same script switches notifyTrackChanges on, leaves the queue on
            the second entry, appends to playback.jsonl (whose request-stage
            counts it then reads cumulatively) and fills the audio cache, so a
            warm re-run serves every byte from disk and logs zero API requests.

Paths are passed in absolute so the reset cannot silently create an empty SQLite
database in whatever directory happened to be current.
"""

import argparse
import datetime
import json
import shutil
import sqlite3
from pathlib import Path

AUDIT053_PROBE_ID = "989999999992"


def settings_row(db: sqlite3.Connection) -> dict:
    return json.loads(db.execute("SELECT metadata FROM local_settings WHERE id=1").fetchone()[0])


def fixture_db(profile: Path) -> sqlite3.Connection:
    db_path = profile / "library.db"
    if not db_path.is_file():
        raise SystemExit(f"fixture database is missing: {db_path}")
    return sqlite3.connect(db_path)


def prune_audit053(profile: Path) -> str:
    db = fixture_db(profile)
    db.execute(
        "DELETE FROM local_record_quarantine WHERE source_table='local_favorites' AND original_id=?",
        (AUDIT053_PROBE_ID,),
    )
    db.commit()
    remaining = db.execute(
        "SELECT count(*) FROM local_record_quarantine WHERE source_table='local_favorites'"
    ).fetchone()[0]
    db.close()
    if remaining != 1:
        raise SystemExit(f"audit053 fixture has {remaining} archived favourites after the prune, expected 1")
    return "audit053 probe archive removed"


def reset_desktop05(profile: Path, archive: Path, cold_cache: bool) -> str:
    db = fixture_db(profile)
    settings = settings_row(db)
    settings["desktop"]["notifyTrackChanges"] = False
    db.execute(
        "UPDATE local_settings SET metadata=? WHERE id=1",
        (json.dumps(settings, ensure_ascii=False, separators=(",", ":")),),
    )
    # A completed run ends on the second queue entry; the next run's first play
    # would then come from the other source and the provider assertion would
    # compare against the wrong track.
    queue = json.loads(db.execute("SELECT metadata FROM queue_snapshot WHERE id=1").fetchone()[0])
    queue["currentEntryId"] = queue["entries"][0]["queueEntryId"]
    queue["currentIndex"] = 0
    queue["positionMs"] = 0
    db.execute(
        "UPDATE queue_snapshot SET metadata=? WHERE id=1",
        (json.dumps(queue, ensure_ascii=False, separators=(",", ":")),),
    )
    db.commit()
    db.close()

    archive.mkdir(parents=True, exist_ok=True)
    stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
    session = archive / stamp
    session.mkdir(exist_ok=True)

    log = profile / "log" / "playback.jsonl"
    if log.exists():
        shutil.move(str(log), str(session / "playback.jsonl"))
        log.parent.mkdir(parents=True, exist_ok=True)
        log.touch()

    cache = profile / "cache" / "audio"
    baseline = archive / "audio-baseline"
    if not baseline.exists() and cache.exists():
        shutil.copytree(cache, baseline)
    if cache.exists():
        shutil.move(str(cache), str(session / "audio"))
    if baseline.exists() and not cold_cache:
        shutil.copytree(baseline, cache)
    return f"desktop05 reset archived under {session.name}; coldCache={cold_cache}"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--audit053-profile", type=Path)
    parser.add_argument("--desktop05-profile", type=Path)
    parser.add_argument("--archive-dir", type=Path)
    parser.add_argument("--cold-cache", action="store_true")
    arguments = parser.parse_args()
    for path in (arguments.audit053_profile, arguments.desktop05_profile, arguments.archive_dir):
        if path is not None and not path.is_absolute():
            raise SystemExit("fixture paths must be absolute")
    notes = []
    if arguments.audit053_profile:
        notes.append(prune_audit053(arguments.audit053_profile))
    if arguments.desktop05_profile:
        if arguments.archive_dir is None:
            raise SystemExit("--archive-dir is required with --desktop05-profile")
        notes.append(reset_desktop05(arguments.desktop05_profile, arguments.archive_dir, arguments.cold_cache))
    print(json.dumps({"notes": notes}))


if __name__ == "__main__":
    main()
