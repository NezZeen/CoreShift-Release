//go:build android

package mobile

// Android 8 to 11 filter the system calls of apps with seccomp, and one they
// do not allow does not fail: the kernel sends SIGSYS, and the process dies.
// Go asks for pidfd_open the first time it starts a program (a core), and
// Android 10 has no such call in its list: CoreShift closed at once on
// Android 10 phones. Go itself catches that SIGSYS and goes on without
// pidfd (issue #69065), but not in a library such as this one, where it
// leaves SIGSYS to the app and has none.
//
// So a handler of our own, set when the library loads, before any Go runs:
// a call that seccomp refused returns ENOSYS ("no such call"), as on a
// kernel without it, and Go falls back to what older Android has (it does
// for pidfd, faccessat2, close_range and the like). Any other SIGSYS keeps
// its usual ending.

/*
#define _GNU_SOURCE
#include <errno.h>
#include <signal.h>
#include <string.h>
#include <ucontext.h>

#ifndef SYS_SECCOMP
#define SYS_SECCOMP 1
#endif

static void coreshift_sigsys(int sig, siginfo_t *info, void *ctx) {
	if (info == NULL || info->si_code != SYS_SECCOMP || ctx == NULL) {
		signal(SIGSYS, SIG_DFL);
		raise(SIGSYS);
		return;
	}
	ucontext_t *uc = (ucontext_t *)ctx;
#if defined(__x86_64__)
	uc->uc_mcontext.gregs[REG_RAX] = -ENOSYS;
#elif defined(__i386__)
	uc->uc_mcontext.gregs[REG_EAX] = -ENOSYS;
#elif defined(__aarch64__)
	uc->uc_mcontext.regs[0] = (unsigned long long)(long long)-ENOSYS;
#elif defined(__arm__)
	uc->uc_mcontext.arm_r0 = (unsigned long)(long)-ENOSYS;
#endif
}

__attribute__((constructor)) static void coreshift_catch_sigsys(void) {
	struct sigaction sa;
	memset(&sa, 0, sizeof sa);
	sa.sa_sigaction = coreshift_sigsys;
	sa.sa_flags = SA_SIGINFO | SA_ONSTACK | SA_RESTART;
	sigemptyset(&sa.sa_mask);
	sigaction(SIGSYS, &sa, NULL);
}
*/
import "C"
