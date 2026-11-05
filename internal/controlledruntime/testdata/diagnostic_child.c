/* Disposable exec/write fixture, never bhyve or a guest workload. */
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/un.h>
#include <sys/wait.h>

#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int
_diagnostic_fail(int i_code, const char *p_phase) {
   int i_error = errno;
   (void)fprintf(stderr, "diagnostic phase=%s code=%d errno=%d (%s)\n",
                  p_phase, i_code, i_error, strerror(i_error));
   (void)fflush(stderr);
   return (i_code);
}

static int
_diagnostic_native_options(int i_enabled) {
#ifdef __FreeBSD__
   int i_fd, i_option;
   for (i_fd = 1; i_fd <= 2; i_fd++) {
      int i_observed = 0;
      socklen_t u_size = sizeof(i_observed);
      int i_result;
      errno = 0;
      i_result = getsockopt(i_fd, SOL_SOCKET, SO_NOSIGPIPE,
                            &i_observed, &u_size);
      if (i_result != 0 || u_size != sizeof(i_observed) ||
          i_observed == 0) {
         (void)fprintf(stderr,
                        "SO_NOSIGPIPE fd=%d result=%d len=%u value=%d "
                        "errno=%d\n", i_fd, i_result, (unsigned)u_size,
                        i_observed, errno);
         return (-1);
      }
      if (!i_enabled && setsockopt(i_fd, SOL_SOCKET, SO_NOSIGPIPE,
                                   &i_enabled, sizeof(i_enabled)) != 0) {
         (void)fprintf(stderr, "disable SO_NOSIGPIPE fd=%d errno=%d\n",
                        i_fd, errno);
         return (-1);
      }
      for (i_option = 0; i_option < 2; i_option++) {
         int i_name = i_option ? SO_SNDBUF : SO_RCVBUF;
         u_size = sizeof(i_observed);
         errno = 0;
         i_result = getsockopt(i_fd, SOL_SOCKET, i_name,
                               &i_observed, &u_size);
         if (i_result != 0 || u_size != sizeof(i_observed) ||
             i_observed < 0 || i_observed > 4096) {
            (void)fprintf(stderr,
                           "socket buffer fd=%d option=%d result=%d "
                           "len=%u value=%d errno=%d\n", i_fd, i_name,
                           i_result, (unsigned)u_size, i_observed, errno);
            return (-1);
         }
      }
   }
   return (1);
#else
   (void)i_enabled;
   return (0);
#endif
}

static int
_diagnostic_negative_signal(void) {
   int i_status;
   pid_t i_child = fork();
   if (i_child == -1)
      return (-1);
   if (i_child == 0) {
      /* Kernel SIGPIPE must kill this disposable writer with default policy. */
      (void)write(STDOUT_FILENO, "negative", 8);
      _exit(95);
   }
   if (waitpid(i_child, &i_status, 0) != i_child ||
       !WIFSIGNALED(i_status))
      return (-1);
   return (WTERMSIG(i_status));
}

int
main(int i_argc, char **p_argv) {
   struct sockaddr_un s_address;
   struct sigaction s_action;
   sigset_t s_unblock;
   char c_command, c_report[256];
   int i_socket, i_native, i_signal = 0;
   int i_stdout = -1, i_stderr = -1, i_size;
   int i_negative;
   pid_t i_parent = getppid();
   if (i_argc != 3)
      return (_diagnostic_fail(80, "argc"));
   /* Bounds only this owned fixture; it never runs a VM. */
   (void)alarm(10);
   i_negative = strcmp(p_argv[2], "default-negative") == 0;
   memset(&s_action, 0, sizeof(s_action));
   s_action.sa_handler = SIG_DFL;
   if (sigemptyset(&s_action.sa_mask) != 0 ||
       sigaction(SIGPIPE, &s_action, NULL) != 0 ||
       sigemptyset(&s_unblock) != 0 ||
       sigaddset(&s_unblock, SIGPIPE) != 0 ||
       sigprocmask(SIG_UNBLOCK, &s_unblock, NULL) != 0)
      return (_diagnostic_fail(81, "default/unblock SIGPIPE"));
   i_native = _diagnostic_native_options(!i_negative);
   if (i_native < 0)
      return (_diagnostic_fail(82, "native socket options"));
#ifdef __linux__
   /* Explicit substituted Linux boundary, never native SO_NOSIGPIPE proof. */
   if (strcmp(p_argv[2], "linux-ignore-boundary") == 0) {
      s_action.sa_handler = SIG_IGN;
      if (sigaction(SIGPIPE, &s_action, NULL) != 0)
         return (_diagnostic_fail(83, "Linux signal boundary"));
   }
#endif
   memset(&s_address, 0, sizeof(s_address));
   s_address.sun_family = AF_UNIX;
#ifdef __FreeBSD__
   s_address.sun_len = sizeof(s_address);
#endif
   if (strlen(p_argv[1]) >= sizeof(s_address.sun_path))
      return (_diagnostic_fail(84, "oracle pathname length"));
   memcpy(s_address.sun_path, p_argv[1], strlen(p_argv[1]) + 1);
   i_socket = socket(AF_UNIX, SOCK_STREAM, 0);
   if (i_socket < 0 || connect(i_socket, (struct sockaddr *)&s_address,
                              sizeof(s_address)) != 0)
      return (_diagnostic_fail(85, "oracle socket/connect"));
   if (write(i_socket, "R", 1) != 1 || write(3, "R", 1) != 1)
      return (_diagnostic_fail(86, "oracle/control readiness write"));
   if (read(i_socket, &c_command, 1) != 1 || c_command != 'W' ||
       getppid() == i_parent)
      return (_diagnostic_fail(87, "oracle release/actual parent exit"));
   if (i_negative) {
      i_signal = _diagnostic_negative_signal();
   } else {
      errno = 0;
      if (write(STDOUT_FILENO, "stdout-after-owner-exit", 23) == -1)
         i_stdout = errno;
      errno = 0;
      if (write(STDERR_FILENO, "stderr-after-owner-exit", 23) == -1)
         i_stderr = errno;
   }
   i_size = snprintf(c_report, sizeof(c_report),
                     "{\"Native\":%d,\"Signal\":%d,"
                     "\"Stdout\":%d,\"Stderr\":%d}\n",
                     i_native, i_signal, i_stdout, i_stderr);
   if (i_size < 0 || (size_t)i_size >= sizeof(c_report) ||
       write(i_socket, c_report, (size_t)i_size) != i_size)
      return (_diagnostic_fail(88, "oracle report write"));
   if (close(i_socket) != 0)
      return (_diagnostic_fail(89, "oracle close"));
   return (0);
}
