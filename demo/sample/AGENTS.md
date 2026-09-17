# Northwind invoicing

The billing service behind Northwind's invoices. Go, one package, no framework.

## Rules for agents

- A comment says why the code exists or why it is written this way, never what the code does.
- Never add a fallback or a silent default: raise the error instead.
- Tests exercise the real repository, never a mock, a stub or a fake.
- No placeholder phrases such as "in a real implementation" or "for now".
- Money is an integer number of cents, never a float.
