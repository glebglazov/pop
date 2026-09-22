---
fragment: 7039F173
generation: 0064
branch: 2026-09-22-planning-sources/02-planning-sources-convention
---

~ Convention kind
  One member of the closed set of things pop can hold a **Repo convention**
  for — `commits`, `implementation`, `issue-tracker`, `planning-sources` and
  `verification`. Closed because each kind ships a **Shipped convention** pop
  must have written, so a kind pop has never heard of has no answer to offer;
  an unknown kind is refused with the list of the ones that exist. A kind also
  declares its **Convention consumption shape**. Names are stable addresses.
  avoid: convention type, convention name
  was: One member of the closed set of things pop can hold a **Repo convention** for — `commits`, `issue-tracker`, `implementation` and `verification`. Closed because each kind ships a **Shipped convention** pop must have written, so a kind pop has never heard of has no answer to offer; an unknown kind is refused with the list of the ones that exist, as `pop config repo set` refuses an unknown key. A kind also declares its **Convention consumption shape**, which is what tells the author of the next kind what they owe it. What makes something a kind at all is the rule in ADR-0247: a convention holds a repository's facts and standards, while a step's procedure is pop's — which is why `refine` was one and is not, its standards having become `implementation` and its procedure having moved into the Refiner's prompt. Names are addresses and stay stable.

~ Convention consumption shape
  Whether a **Convention kind** reaches an agent as a prompt body or as a
  labelled block. `verification` is role-driving. `commits`, `implementation`,
  `issue-tracker` and `planning-sources` are step-informing.
  avoid: prompt shape, envelope, injection mode
  was: Whether a **Convention kind** reaches an agent as a prompt *body* or as a labelled *block*, declared by the kind and honoured at every call site (ADR-0227). A **role-driving** kind — `verification`, and after ADR-0247 only `verification` — is an agent's entire mandate, so the convention is the body and pop supplies only a **Role preamble** and a **Response contract** around it; there is then exactly one voice on what to check. A **step-informing** kind — `commits`, `issue-tracker`, `implementation` — is a fact a prompt about something else needs, so it stays a block inside pop's prompt. The shape names a kind's *mandate-bearing* consumption, not its only one.
