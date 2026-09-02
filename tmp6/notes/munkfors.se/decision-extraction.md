# Munkfors KF decision extraction guidance

- KF protocols present each paragraph with "Beslut" (often headed
  "Kommunfullmäktiges beslut") or as pure information items.
- Keep as decisions: upprop (godkänns), val av justerare, godkännande av
  föredragningslista, detaljplaneantaganden, medborgarförslag outcomes (avslås),
  årsredovisning + ansvarsfrihet (all numbered beslut points in full_text,
  jäv-notering may stay), and anmälningsärenden where the beslut explicitly
  states "Redovisade ärenden anses anmälda och läggs till handlingarna".
- Skip pure information paragraphs (§ "Information ..."/"Informationsärenden")
  that end with "Ordförande tackar för informationen" and have no Beslut block.
- No counted omröstningar appear; omit voting_method unless one is recorded.
