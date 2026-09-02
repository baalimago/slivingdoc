# Ragunda KF decision extraction guidance

- Extract only items under a "Kommunfullmäktiges beslut" heading with a substantive
  outcome (godkänna, fastställa, avslå, remittera, välja, uppdra).
- Skip paragraphs whose beslut only "tar del av" information (informationer,
  uppföljning/redovisning, meddelanden) even when headed Beslut.
- Skip "Allmänhetens frågestund" (ajournering), "Dialog med revisorerna" (no info),
  and interpellation/fråga sessions (att ställa tillåts är inte formellt beslut).
- Procedural § (protokollets justering/upprop/dagordning, val av justerare) is a decision.
- Redovisning § that also contains a substantive uppdrag (e.g. uppdra åt KS att
  besvara) keep the full text of both parts.
- Valärenden is one § with many numbered decisions; keep as one entry with full text
  of all items; a closed/counted omröstning inside goes into voting_method.
- Record explicit counted voteringar in voting_method (ja/nej counts); chair-led
  propositions without counts → omit voting_method.
