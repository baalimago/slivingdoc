# aneby.se KF decision extraction guidance

- Aneby KF protocols use a "Beslut" block per paragraph; the decision text follows
  "Kommunfullmäktige beslutar att ...".
- Skip paragraphs whose beslut only says "lägga informationen till handlingarna":
  Tematimme/informationspunkter, Anmälningsärenden, and Svar på interpellation
  (interpellation answers are filed as information, not a decision).
- Keep report paragraphs whose beslut contains additional formal action beyond
  filing information, e.g. "notera ... samt anmäla beslutet till kommunrevisionen"
  (IVO report items).
- Keep procedural decisions: upprop, val av justerare, godkännande av
  föredragningslista, and handling of incoming medborgarförslag/motioner/
  interpellationer (överlämna till kommunstyrelsen för beredning).
- Nuance on upprop: include it only when its Beslut block carries an explicit
  "Kommunfullmäktige beslutar" wording. Some protocols only state "Ordförande
  öppnar mötet" under § Upprop — that is a meeting-opening record, not a
  decision outcome; skip it.
- When a motion is denied, the decision text is "ställa sig bakom upprättat svar ...
  att motionen avslås med hänvisning till svarsskrivelsen"; record a reservation if
  one is noted (e.g. "Reserverar sig mot beslutet").
- Voting: Aneby KF decisions list "Yrkanden" (bifall) but no omröstning unless stated;
  omit voting_method when no vote is recorded.
