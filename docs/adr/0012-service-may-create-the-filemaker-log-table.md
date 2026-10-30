# 12. The service may create the FileMaker log table, when told to

- **Status:** Accepted (built in #113)
- **Tickets:** #113

## Context

With `STORAGE=filemaker`, request logs go to a FileMaker table (`FMS_LOG_TABLE`, default `Logs`). Until now the service never created or changed it: a FileMaker developer set it up by hand, and a missing table stopped startup. Setting up a new FileMaker file, or a test file, then needs a developer before the service can start, although the table is simple and fixed.

## Decision

- **Opt-in:** with `FMS_CREATE_LOG_TABLE=true`, startup creates the log table when it is missing. It is off by default, and off nothing changes. Creating a table needs schema privileges, which the account that writes logs shouldn't need otherwise.
- **Only a missing table:** the table is created only when FileMaker answers the startup check with 404 and its code `-1020` ("table not defined"). Any other error — a missing or wrong field, a wrong password, an unreachable server — still stops startup, and an existing table is never changed.
- After creating it, startup checks the table again, and logs a warning that it created it. A failed create stops startup with FileMaker's error.
- The table gets the fields the service reads and writes. OData can't create an auto-enter serial, so `ID` stays empty until a developer makes it one in FileMaker; the service never reads it back to find a record.
- The FileMaker replica's tables ([ADR 10](0010-sqlite-master-filemaker-replica.md)) are still never created by the service.

## Consequences

- A new file can log requests without a developer, if the account is given schema privileges for the first start. Taking them away afterwards is up to the person who set it up.
- A created table has no indexing settings or auto-enter `ID`, unlike one set up by hand; a developer can add them later without the service noticing.
