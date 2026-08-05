# Layer Boundary Checklist

## Ownership

| Layer | Owns | Must not own |
|---|---|---|
| Controller / endpoint | Routing, binding, auth guard, headers, status, problem details, transaction marker | Product workflow, direct database, provider SQL, schema, permission graph |
| Application service | Product use case, DTO validation/mapping, permission intent | Provider SQL/dialect, connection/transaction mechanics, shared algorithms |
| Manager / domain service | Reusable operations and invariants | Transport concepts and route-specific presentation |
| Repository / adapter | Persistence implementation and product-to-infrastructure mapping | Public transport contract, product authorization policy, workflow coordination |
| Provider infrastructure | SQL/DDL, parameterization, provider clients, connection plumbing | Hidden product workflow or release semantics |
| Public contracts | DTOs and application interfaces | Internal ports, repositories, stores, contexts, query plans |

Dependencies point inward: transport to contracts/application, application to domain/shared abstractions, infrastructure implementing inward ports, and provider-neutral projects never referencing provider implementations.

## Dependency Injection

- Use the repository's normal module scanning for ordinary services.
- Hand-register external SDK adapters, provider selection, framework factories, feature switches, and test replacements only when needed.
- Constructor-require dependencies whose absence changes security, persistence, or runtime behavior.
- Keep module methods as composition roots, not duplicate service catalogs.

## Flags

- Endpoint/controller injects a connection, context, repository, or several low-level managers.
- Application service directly depends on a repository for a multi-step product/runtime workflow.
- One repository owns context, authorization, query planning, SQL, and execution.
- Intermediate contracts pass final SQL fragments or raw parameter dictionaries.
- Product code quotes identifiers, builds dialect SQL, or opens a transaction beneath a claimed unit of work.
- Obsolete compatibility adapters remain after the consumer migrated.

Schema/seed tools, database administration, isolated report runners, and explicitly provider-specific features are valid only as narrow exceptions.

## Useful Scans

```powershell
rg -n "Repository|Store|Context|Connection|Dialect" <contracts-or-application-root>
rg -n "BeginTransaction|Create Table|Alter Table|WhereSql|JoinSql|OrderBySql" <application-or-http-root>
rg -n "AddScoped|AddSingleton|AddTransient|ServiceDependency" <module-root>
```

Convert each match into an ownership decision; a text match alone is not a defect.
