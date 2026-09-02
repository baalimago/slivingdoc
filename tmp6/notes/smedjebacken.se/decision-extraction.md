# Smedjebacken (smedjebacken.se) - KF decision extraction notes

- KF protocols follow the standard structure: each § has "Kommunfullmäktiges beslut"; extract those blocks verbatim.
- Skip information items even when they carry a Beslut block: ESF-projekt info ("Informationen tas emot och läggs till handlingarna"), ekonomisk uppföljning ("Informationen godkänns"), delgivningar ("Delgivningarna tas emot och läggs till handlingarna").
- Quirk: the delgivningar § header in the KF protocol says "Kommunstyrelsens beslut" even though it is a KF protocol; treat it as a KF information item, not a KS decision.
- Keep election/fyllnadsval paragraphs (including "Frågan återremitteras"), motion decisions with reservations, and avsägelse decisions.
- Chair-led proposition without recorded omröstning -> omit voting_method.
