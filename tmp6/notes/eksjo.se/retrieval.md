# eksjo.se retrieval log

## kf — 2026-08-20: SUCCESS, 42 protocols recorded (2022-02-24 .. 2026-06-16)

- Single source page (live): Kommunfullmäktige page under
  /kommun-och-politik/politik-och-paverkan/kommunfullmaktige-namnder-utskott-styrelser-och-revision/kommunfullmaktige.
- slim_http returns 2023/2024/2025 links (static file portlets) but NOT 2026 (file-share webapp)
  and NOT 2022 (section removed). Downloaded page HTML -> AppRegistry.registerInitialState JSON
  holds the 2026 files (folderId 19.61dbeb4317ddd5cd047e71a): 01-08, 02-19, 03-19, 04-23, 05-21,
  06-16 (all PDF-verified).
- 2022: live /download/ URLs 404. Wayback CDX + 2023-02-01 / 2024-05-08 captures of the KF page
  list the 10 2022 protocols with the same node URLs. Downloaded via
  https://web.archive.org/web/<ts>id_/<original-url>; all 10 verified SAMMANTRÄDESPROTOKOLL
  Kommunfullmäktige with matching sammanträdesdatum. Best capture timestamps:
  02-24 20240303070127, 03-24 20220616205836 (20240303 capture truncated), 04-21 20240331054148,
  05-19 20240303065432, 06-14 20220619085214 (20240303 capture truncated), 09-29 20240331053938,
  10-17 20240303070315, 10-27 20240331053904, 11-24 20240331053943, 12-15 20240331053910.
- 2023-2026: every PDF downloaded from live eksjo.se and text-verified (date inside matches).
- Recorded: 2022 x10 (Wayback URLs, conf 0.9), 2023 x9, 2024 x8, 2025 x9, 2026 x6 (live, conf 0.97).
- Notable: 2024-03-19 file = 2024-03-21 meeting (recorded 2024-03-21); 2025-12-04 "paragraf 228"
  supplement skipped; 2026-08-20 meeting (today) has only tillkännagivande, no protocol yet.

## Notebook infra note
- slivingdoc notes pull/commit repeatedly failed with "local private state operation failed"
  during this run; notes were written to disk but the service state would not accept them.
