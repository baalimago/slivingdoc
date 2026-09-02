# Alingsås kommun KF – decision extraction guidance

- The protocol text is authoritative for municipality identity. Alingsås KF
  protocols are identifiable by "Alingsås kommun" plus local entities such as
  Alingsåshem AB, AB Alingsås Rådhus, Alingsås Energi AB and Alingsås och
  Vårgårda Räddningstjänstförbund.
- Contamination-sweep flags naming a different municipality (e.g. Arvika) are
  false positives when the protocol text consistently references Alingsås;
  keep organization_name "Alingsås kommun" and re-derive the meeting from the
  document rather than rejecting it.
- Party abbreviations used in Alingsås KF protocols (M, S, SD, C, V, KD, L,
  MP, -) are all already mapped in canonicalization.md.
