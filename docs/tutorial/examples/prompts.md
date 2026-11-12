## New Scenario
- I will describe a scenario.
- Keep a running conversation log in Conversation-YYYY-MM-DD.md (and split per day) and prepare a Plan.md and a Spec.md, also prepare test cases as we go.
- Keep the Spec.md, and Plan.md up-to-date and regularly after significant changes suggest that it is time for a crosscheck between the Spec.md, Plan.md and the actual code existing.
- When i type a sentence and you see it contains errors or uncertainties, first suggest corrections and confirm with me before proceeding.
- The files go in a architecture folder unless that already exists, then ask.
- if this is a new project without git, create a new git repo and add all of the above to the git repo.
- make a explicit Changes.md where we will record the changes between releases

## Graphic app restrictions
- When planning any `Graphic user interface` or `Text user interface` component, use a default white background with black text and apply a sufficient contrast ratio >4.5 on all color combinations. Allow the user to increase the contrast index interactively or via a config file.


## GithubActions
When using github actions or other action steps,
pin them on the version hash and try to use the latest workable version.
Explain when that is not possible.


## Creative-Pedantic
- When you are in research mode or investigating a new request from the user,
  you may be creative but must rely on proven or existing solutions, show references.
  If no solution can be found, you must ask the user if you can switch a more loosely creative mode,
  when allowed you may suggest possible approaches but also say that you invented it or that it is
  just a suggestion without existing current proof or usage.
- When you are verifying or executing a crosscheck,  you are not allowed to be creative,
  but should always rely on existing facts currently in the items you are investigating.
  Pedantic reporting is here the modus operandi.


## Correct my text first
When i type a sentence and you see it contains errors or uncertainties, first suggest corrections and confirm with me before proceeding.


## CrossCheck
when i say "do a crosscheck" or "crosscheck": Do the following:
- inspect all code and match against the Spec and Plan.
- inspect all the Spec and Plan and check against the code.
- verify that all examples follow code and the Spec and Plan.
- run the test suite and report which tests pass or fail.
- Save in a crosscheck file architecture/Crosscheck-YYYY-MM-DD-nn.md, with a two-digit number that restarts at 01 each day.
- add missing examples if you find reason to add some.
- Suggest other improvements.
- For mismatches between code and docs, Claude proposes the fix and waits for the user's OK, before changing either side.
- a crosscheck also checks TestCases.md against the code and the actual tests.
- Report in chat. Besides saving the file, also post a short summary in chat.


## Cleanup
- All projects should maintain a list of the temp files they create
  (i suggest a named subdir in /tmp with the session name or project name)
  and should cleanup the temp files as soon as they are no longer needed.
- The same goes for docker images
  (best write a list of images used during development in the project)
  and at each release investigate what images can be removed and what volumes can be cleaned up.
