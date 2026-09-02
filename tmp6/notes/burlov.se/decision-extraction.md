# Burlöv KF decision extraction guidance

- KF protocols use the standard "Kommunfullmäktige beslutar att ..." block per §.
  Extract those blocks verbatim, keeping all "att"-points.
- § "Val av justerande och datum för justering" is a decision - include.
- Avsägelser och fyllnadsval are decisions - include.
- Items whose decision is only "läggs ... till handlingarna" (delårsrapport,
  delgivningar, anmälningar) are information/filing items - skip, even though
  they appear as protocol "beslut".
- Interpellation paragraphs (Q&A, answered or deferred) are not decisions - skip.
- For motions with recorded votering: include the tally in voting_method, e.g.
  "Votering (22 ja, 19 nej)"; outcome "Avslag"/"Bifall". Reservations,
  "Beslutsgång" and yrkanden are not part of the decision text - skip.
- Politicians: include beslutande ledamöter, tjänstgörande ersättare (tag
  "ersätter X (parti)") and ej tjänstgörande ersättare (tag
  "ej tjänstgörande ersättare"). Staff (sekreterare, kommundirektör) are not
  politicians; skip. Meeting venue is in Arlöv but municipality is Burlöv.
