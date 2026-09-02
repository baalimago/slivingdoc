# Gagnef KF decision extraction guidance

- Standard KF protocol structure: each § has "Kommunfullmäktiges beslut" numbered points; extract those points verbatim into full_text, keep tables in a readable transcription.
- Skip information items even when they have a Beslut block whose only content is filing/noting: "Lägga informationen till handlingarna" (ekonomisk rapport), "Lägga rapporterna som information till protokollet" (verksamhetsrapport, rapporter), "Lägga redovisningen till protokollet" (redovisning av motioner/medborgarförslag), "Anmälan av kommunstyrelsens protokoll".
- Election paragraphs are decisions and are kept, including "Avvakta val" (postponed election) outcomes — outcome "Avvaktas". Val av revisor/suppleant, avsägelser (bevilja) are also decisions.
- No counted omröstning in these protocols; chair-led "Beslutsgång" without recorded vote -> omit voting_method.
- "Deltar ej i beslut" (e.g. all KD ledamöter in budget §) and "Jäv" notes are part of the decision record; keep them in full_text.
- Politicians: include beslutande ledamöter, tjänstgörande ersättare and närvarande ersättare; use jäv/tjänstgörande-for tags as identifiable_tags.
