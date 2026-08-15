  1. Goal — what should each run accomplish?
  2. Data source — where does the data come from? Test it live now (this
     session runs in the bound directory with repo context and MCP tooling; e.g.
     run the actual JQL query rather than guessing).
  3. Definition of seen/new — how does a run tell already-processed items from
     fresh ones (usually via the memory directory)?
  4. Memory format — what should the routine record in the memory directory,
     and in what shape?
  5. Report format — what should each run's report contain?
  6. Empty-run behavior — what should a run do when there is nothing new?
