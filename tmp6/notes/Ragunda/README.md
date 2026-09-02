# Ragunda - scanner notes location

The scoped directory for "Ragunda" runs is /tmp/sakfraga-notebook/notes/Ragunda/,
but it is normally empty; Ragunda's actual worklog files live under
/tmp/sakfraga-notebook/notes/ragunda.se/ (kf-retrieval.md covers MeetingPlus
retrieval and KF meeting dates).

## KF decision extraction (Ragunda protocol format)
- Paragraphs numbered "KF § N" in the source; keep that prefix in paragraph_number.
- Protocol has an "Information" section (KF § 20-29) and a "Beslut" section
  (KF § 30+). Procedural items inside the Information section that say
  "Kommunfullmäktige beslutar" (val av justerare, fastställande av dagordning)
  are still formal decisions and should be extracted.
- A paragraph headed "Kommunfullmäktiges beslut" whose text only says the item
  "utgår från sammanträdet och flyttas till kommande kommunfullmäktige"
  (deferral) is extracted as a bordläggning decision.
- No counted omröstningar appear in this protocol type; omit voting_method
  unless one is recorded.
