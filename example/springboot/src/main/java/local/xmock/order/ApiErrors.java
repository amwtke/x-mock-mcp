package local.xmock.order;

import org.springframework.dao.TransientDataAccessException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.server.ResponseStatusException;

@RestControllerAdvice
public class ApiErrors {
  public record Error(String message) {}
  @ExceptionHandler(ResponseStatusException.class)
  public ResponseEntity<Error> status(ResponseStatusException error) {
    return ResponseEntity.status(error.getStatusCode()).body(new Error(error.getReason()));
  }
  @ExceptionHandler(TransientDataAccessException.class)
  public ResponseEntity<Error> conflict(TransientDataAccessException error) {
    return ResponseEntity.status(HttpStatus.CONFLICT).body(new Error("订单与其他操作冲突，请重试"));
  }
}
