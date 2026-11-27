# Privacy and terms for a self-hosted instance

Transferly exposes `/legal` before sign-in and links to it from login, setup, invitations, browser approval and Settings. The notice describes what the software does. The operator supplies the instance-specific notice and terms, and is the contact for requests about data held by the instance.

Publish the operator's notice where users can read it before sharing their email or approving a terminal session. Include the operator's identity and contact details, purposes and applicable legal bases, recipients and service providers, retention periods or criteria, and how to exercise applicable rights or make a complaint. Cover the instance's backups, proxy and access logs, and any activity visible to administrators. Describe optional connectors and any transfers to other jurisdictions that the operator enables.

The stock web application uses one session cookie and stores preferences in the browser. Its notice identifies both. Inventory anything added by the deployment, such as embedded content, tracking, analytics or scripts injected by a hosting provider. Where local rules require consent for added storage or tracking, keep it disabled until consent is granted and provide a way to withdraw consent.

The terminal notice is available through `transferly-tui privacy` and listed in terminal help. Document how users can end or revoke a terminal session and where the operator publishes contact details. The terminal sends its client name and hostname for browser approval. Treat these values, alongside saved connection metadata, activity and session records, as part of the instance's inventory.
