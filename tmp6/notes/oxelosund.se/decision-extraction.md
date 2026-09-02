# Oxelösund KF decision extraction guidance

- Protocol layout: each paragraph has a "Kommunfullmäktiges beslut" block followed by
  Sammanfattning, Beslutsunderlag, Dagens sammanträde, Kommunstyrelsens förslag,
  Förslag, Beslutsgång. Extract only the beslut block content.
- Paragraph numbers are written "Kf §N" in the source; keep that prefix.
- Skip pure information paragraphs even when they carry a beslut "Godkänna
  informationen" (e.g. information/föredragningar, information from KS ordförande).
- Skip question-session paragraphs: "Allmänhetens frågestund" and "Frågor till
  kommunfullmäktige" (records questions asked/answered, no substantive decision).
- Delgivningar with beslut "läggs till handlingarna" is a recorded decision; include.
- Valärenden/avsägelser paragraph where beslut notes "inga inkommit" is recorded as
  a konstaterande decision; include.
- Motions paragraphs: "medger att motionen får lämnas in för beredning" is a
  decision; include. Motion with "avslås och anses besvarad" is a decision; include.
- Återremiss (incl. minoritetsåterremiss) is a decision; the beslut block text is the
  remittering instruction.
- No counted omröstningar in the sampled protocols; omit voting_method unless an
  explicit ja/nej/avstår count appears.
