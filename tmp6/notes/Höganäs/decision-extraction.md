# Höganäs KF decision extraction guidance

- Höganäs KF protocols use the standard "Beslut" block per §; extract those blocks verbatim, keeping all "att"-points.
- Skip "Anmälan om ny ersättare" items whose beslut is only "att lägga anmälan till handlingarna" (announcement/filing, not a decision).
- Budget paragraphs (§ "Antagande av budget") contain counted omröstningar on each amendment (yrkande). Each recorded votering is an explicit decision outcome: extract each as a separate decision with outcome "Avslag" (or "Bifall") and voting_method including the tally (e.g. "Votering (26 ja, 14 nej, 0 avstår)"); the final "Beslut" block is the main decision for the paragraph.
- Reservations ("Reserverar sig...") and "Beslutsgång" (chair proposition) are not part of the decision text; skip them.
- Interpellation paragraphs whose beslut is "att interpellationen får ställas" are decisions; keep them.
- Politicians: include beslutande ledamöter, tjänstgörande ersättare (tag "ersätter X (parti)") and ej tjänstgörande ersättare (tag "ej tjänstgörande ersättare"). Staff (sekreterare, kanslichef) are not politicians; skip.
