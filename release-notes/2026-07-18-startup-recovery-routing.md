### Startup and repair recovery follows verified feature state

`boatstack run` now enters planning when one saved Plan-mode file exists and explains how to start when none exists. Repair routes pre-build work to the verified planning or build step instead of asking for a change without an approved baseline. Invalid or orphaned delivery evidence is reported without clearing artifacts, and Cursor's `MainThreadShellExec not initialized` failure now points to window reload rather than unnecessary Boatstack reinstallation.
