# Gagnef KF decision extraction guidance

- Gagnef KF protocols list each case as "KF § N" with a "Kommunfullmäktiges
  beslut" block of numbered points. Extract those blocks as decisions.
- Skip information items even with a beslut block: "Lägga informationen till
  handlingarna", "Lägga rapporterna som information till protokollet",
  "Lägga redovisningen till protokollet" (verksamhets-/ekonomiska rapporter,
  redovisning av gamla motioner).
- Skip "Anmälan av kommunstyrelsens protokoll" ("Godkänna redovisningen av
  protokollen") as an announcement, and ajournering paragraphs as procedural.
- Interpellations record "X svarar på interpellationen" as the beslut - include.
- Medborgarförslag/motion dispositions (remittera, överlämna till nämnd för
  avgörande, avslå) are decisions.
- Ansvarsprövning series (§ per nämnd): each is its own decision; "Frågan om
  ansvarsfrihet ska avgöras idag" (procedural vote on deferral) is also a
  recorded decision with reservation - include.
- Avsägelse + fyllnadsval paragraphs are decisions (bevilja avsägelse + välja
  ersättare); "Avvakta val" is also a decision, keep.
- Voting: chair-led proposition ("Ordföranden ställer ... och finner att")
  without counts -> omit voting_method; reservations are not part of the
  decision text.
