# Security Policy

We take security seriously at Entire. We appreciate your efforts to responsibly disclose vulnerabilities and will make every effort to acknowledge your contributions.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please send security-related reports to **[security@entire.io](mailto:security@entire.io)**.

### What to Include

When reporting a vulnerability, please include:

1. **Description** - A clear description of the vulnerability
2. **Impact** - What an attacker could achieve by exploiting this issue
3. **Steps to reproduce** - Detailed steps to reproduce the vulnerability
4. **Affected versions** - Which versions of entire-rsl are affected (if known)
5. **Suggested fix** - If you have ideas on how to fix it (optional)

### What to Expect

- **Acknowledgment** - We will acknowledge receipt of your report within 48 hours
- **Updates** - We will keep you informed of our progress as we investigate
- **Resolution** - We aim to resolve critical vulnerabilities within 90 days

## Confidentiality

**All reports will be kept confidential.** We will not share your information with third parties without your consent, except as required by law.

## Supported Versions

We recommend always running the latest version of entire-rsl.

## Scope

entire-rsl reports whether a repository's gittuf Reference State Log verifies. Its output is a security statement, so the following are in scope in particular:

- Any case where entire-rsl reports `verified` for a ref whose RSL history does not pass gittuf verification, has no applicable policy, or could not be fully evaluated
- Terminal escape or control sequences from repository data reaching the terminal through text output
- Any write to refs, objects or git config other than the remote-tracking RSL refs written by a confirmed fetch
- Argument or option injection into the git subprocesses entire-rsl runs

### Out of Scope

The following are generally not considered security vulnerabilities:

- Issues in third-party dependencies, including gittuf itself (please report these upstream)
- The contents of RSL custom fields, which entire-rsl labels as asserted and never uses for a verification outcome
- Social engineering attacks
- Denial of service attacks
- Issues requiring physical access to a user's device

---

Thank you for helping keep Entire and our community safe!
